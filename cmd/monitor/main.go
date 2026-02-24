package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/jaycchiu524/campsite-notifier/internal/scraper"
	"github.com/joho/godotenv"
	stealth "github.com/jonfriesen/playwright-go-stealth"
	"github.com/playwright-community/playwright-go"
	"gopkg.in/yaml.v3"
)

type Target struct {
	Park      string `yaml:"park"`
	Arrival   string `yaml:"arrival"`
	Nights    int    `yaml:"nights"`
	Equipment string `yaml:"equipment"`
}

type YAMLConfig struct {
	Targets []Target `yaml:"targets"`
}

type Config struct {
	Interval    int    `env:"CHECK_INTERVAL_MINUTES" envDefault:"10"`
	IsDev       bool   `env:"DEV" envDefault:"false"`
	TargetsFile string `env:"TARGETS_FILE" envDefault:"targets.yaml"`
	ReportFile  string `env:"REPORT_FILE" envDefault:"report.yaml"`
}

type SearchResult struct {
	Park    string              `yaml:"park"`
	Arrival string              `yaml:"arrival"`
	Results map[string][]string `yaml:"results"`
	Error   string              `yaml:"error,omitempty"`
}

func main() {
	// Load the .env file
	godotenv.Load()

	cfg := Config{}
	if err := env.Parse(&cfg); err != nil {
		log.Fatalf("unable to parse env: %v", err)
	}

	// 1. Initialize Structured Logging
	var handler slog.Handler
	if cfg.IsDev {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	} else {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	// 2. Load targets from YAML
	yamlFile, err := os.ReadFile(cfg.TargetsFile)
	if err != nil {
		logger.Error("Could not read targets file", slog.String("path", cfg.TargetsFile), slog.String("error", err.Error()))
		os.Exit(1)
	}

	var yCfg YAMLConfig
	if err := yaml.Unmarshal(yamlFile, &yCfg); err != nil {
		logger.Error("Could not parse targets file", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// 3. Expand targets
	expandedTargets := expandTargets(yCfg.Targets)

	logger.Info("Monitoring started",
		slog.Int("targets_expanded", len(expandedTargets)),
		slog.Int("targets_original", len(yCfg.Targets)),
		slog.Int("interval_minutes", cfg.Interval),
		slog.Bool("dev_mode", cfg.IsDev))

	// 4. Initialize Playwright
	pw, err := playwright.Run()
	if err != nil {
		logger.Error("Could not start playwright", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pw.Stop()

	// 5. Launch Browser
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(!cfg.IsDev),
		Args: []string{
			"--disable-blink-features=AutomationControlled",
		},
	})
	if err != nil {
		logger.Error("Could not launch browser", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer browser.Close()

	for {
		var wg sync.WaitGroup
		resultsChan := make(chan SearchResult, len(expandedTargets))

		for _, target := range expandedTargets {
			wg.Add(1)
			go func(t Target) {
				defer wg.Done()

				// Create a contextual logger for this target
				tLog := logger.With(slog.String("park", t.Park), slog.String("arrival", t.Arrival))

				res, err := runSearch(tLog, browser, t)
				var errStr string
				if err != nil {
					tLog.Error("Search failed", slog.String("error", err.Error()))
					errStr = err.Error()
				}
				resultsChan <- SearchResult{
					Park:    t.Park,
					Arrival: t.Arrival,
					Results: res,
					Error:   errStr,
				}
			}(target)
		}

		// Close channel when all goroutines are done
		go func() {
			wg.Wait()
			close(resultsChan)
		}()

		var allResults []SearchResult
		for res := range resultsChan {
			allResults = append(allResults, res)
		}

		// Write report to YAML
		reportData, err := yaml.Marshal(allResults)
		if err != nil {
			logger.Error("Error generating report data", slog.String("error", err.Error()))
		} else {
			if err := os.WriteFile(cfg.ReportFile, reportData, 0644); err != nil {
				logger.Error("Error writing report file", slog.String("path", cfg.ReportFile), slog.String("error", err.Error()))
			} else {
				logger.Info("Report generated successfully", slog.String("path", cfg.ReportFile))
			}
		}

		if cfg.IsDev {
			logger.Info("Dev mode enabled: exiting after one run")
			break
		}

		logger.Info("Waiting for next interval", slog.Int("minutes", cfg.Interval))
		time.Sleep(time.Duration(cfg.Interval) * time.Minute)
	}
}

func runSearch(log *slog.Logger, browser playwright.Browser, t Target) (map[string][]string, error) {
	// 1. Create isolated context for each search
	context, err := browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent: playwright.String("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"),
	})
	if err != nil {
		return nil, fmt.Errorf("could not create context: %w", err)
	}
	defer context.Close()

	page, err := context.NewPage()
	if err != nil {
		return nil, fmt.Errorf("could not create page: %w", err)
	}

	// Apply stealth
	stealth.Inject(page)

	// 2. Navigate and handle consent
	log.Debug("Navigating to Ontario Parks")
	if _, err = page.Goto("https://reservations.ontarioparks.ca/", playwright.PageGotoOptions{
		Timeout: playwright.Float(60000),
	}); err != nil {
		return nil, fmt.Errorf("could not goto: %w", err)
	}

	consentButton := page.Locator("button#consentButton")
	if count, _ := consentButton.Count(); count > 0 {
		log.Debug("Handling cookie consent")
		consentButton.First().Click()
		time.Sleep(500 * time.Millisecond)
	}

	// 3. Parse date
	arrival, err := time.Parse("2006-01-02", t.Arrival)
	if err != nil {
		return nil, fmt.Errorf("invalid arrival date %s: %w", t.Arrival, err)
	}

	park := scraper.ParkName(t.Park)

	// 4. Perform check
	res, err := scraper.CheckAvailability(log, page, park, arrival, t.Nights, t.Equipment)
	if err != nil {
		return nil, fmt.Errorf("availability check failed: %w", err)
	}

	return res, nil
}

func expandTargets(targets []Target) []Target {
	var expanded []Target
	validParks := scraper.ValidParks()

	for _, t := range targets {
		found := false
		for _, vp := range validParks {
			if strings.HasPrefix(string(vp), t.Park) {
				expanded = append(expanded, Target{
					Park:      string(vp),
					Arrival:   t.Arrival,
					Nights:    t.Nights,
					Equipment: t.Equipment,
				})
				found = true
			}
		}
		if !found {
			// If no prefix match, keep as is (maybe it's a park not in our list yet)
			expanded = append(expanded, t)
		}
	}
	return expanded
}
