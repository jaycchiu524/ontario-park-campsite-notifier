package scraper

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

/**
1. Click id: park-autocomplete-field - For park selection
2. Select park from the dropdown: div[role="listbox"][aria-labelledby="park-autocomplete-label"] > mat-option[role="option"]
3. Check if the park name matches the selected park


*/

func CheckAvailability(log *slog.Logger, page playwright.Page, park ParkName, arrivalDate time.Time, nights int, equipment string) (map[string][]string, error) {
	log.Info("Starting availability check", slog.String("park", string(park)), slog.Time("arrival", arrivalDate), slog.Int("nights", nights))

	// 1. Navigate to the reservation page if not already there
	if page.URL() != "https://reservations.ontarioparks.ca/" {
		log.Debug("Navigating to reservations page")
		if _, err := page.Goto("https://reservations.ontarioparks.ca/"); err != nil {
			return nil, fmt.Errorf("could not navigate to reservations page: %w", err)
		}
	}

	log.Debug("Entering park name")
	parkInput := page.Locator("input#park-autocomplete-input")
	if err := parkInput.Fill(string(park)); err != nil {
		return nil, fmt.Errorf("could not fill park input: %w", err)
	}
	// Wait for the autocomplete panel and click the option that matches the park name
	optionSelector := fmt.Sprintf("mat-option:has-text(\"%s\")", park)
	if err := page.Locator(optionSelector).First().Click(); err != nil {
		return nil, fmt.Errorf("could not click park option: %w", err)
	}

	log.Debug("Selecting equipment", slog.String("type", equipment))
	if err := page.Locator("mat-select#equipment-field").Click(); err != nil {
		return nil, fmt.Errorf("could not click equipment select: %w", err)
	}
	if err := page.Locator(fmt.Sprintf("mat-option:has-text(\"%s\")", equipment)).First().Click(); err != nil {
		return nil, fmt.Errorf("could not select '%s': %w", equipment, err)
	}

	log.Debug("Selecting dates")
	departureDate := arrivalDate.AddDate(0, 0, nights)

	// Open the calendar
	if err := page.Locator("input#arrival-date-field").Click(); err != nil {
		return nil, fmt.Errorf("could not open calendar: %w", err)
	}

	// Helper to select a date in the calendar
	selectDate := func(targetDate time.Time) error {
		// Format target date as "Month Day, Year" e.g. "February 26, 2026"
		targetLabel := targetDate.Format("January _2, 2006")
		// Remove leading space in day for single digits if necessary, Wait, time.Format "January _2" uses a space for single digits.
		// "January 2" would be better if that's what's in aria-label.
		// Let's re-check aria-label format: "February 26, 2026". "February 6, 2026" would likely be "February 6, 2026" (no leading space).
		targetLabel = fmt.Sprintf("%s %d, %d", targetDate.Month().String(), targetDate.Day(), targetDate.Year())

		for i := 0; i < 12; i++ { // Try for up to 12 months
			// Check if the day is visible
			dayLocator := page.Locator(fmt.Sprintf("button.mat-calendar-body-cell[aria-label=\"%s\"]", targetLabel))
			if count, _ := dayLocator.Count(); count > 0 {
				return dayLocator.First().Click()
			}

			// Not found, click next month
			// Ensure we don't go too far back/forward. Here we only go forward.
			nextButton := page.Locator("button#nextYearButton") // Browser subagent pointed this out
			if count, _ := nextButton.Count(); count == 0 {
				// Fallback to class name if ID is different
				nextButton = page.Locator("button.mat-calendar-next-button")
			}
			if err := nextButton.Click(); err != nil {
				return fmt.Errorf("could not click next month button: %w", err)
			}
			time.Sleep(300 * time.Millisecond) // Wait for transition
		}
		return fmt.Errorf("could not find date %s in calendar after 12 months", targetLabel)
	}

	log.Debug("Picking arrival and departure")
	if err := selectDate(arrivalDate); err != nil {
		return nil, fmt.Errorf("could not select arrival date: %w", err)
	}

	if err := selectDate(departureDate); err != nil {
		return nil, fmt.Errorf("could not select departure date: %w", err)
	}

	// 6. Search
	log.Info("Performing search")
	if err := page.Locator("button#actionSearch").Click(); err != nil {
		return nil, fmt.Errorf("could not click search button: %w", err)
	}

	// Wait for navigation or results to load
	if err := page.WaitForURL("**/campsites/**", playwright.PageWaitForURLOptions{
		Timeout: playwright.Float(5000),
	}); err != nil {
		log.Warn("URL did not change to results page as expected", slog.String("error", err.Error()))
	}

	// 7. Process Search Results
	log.Debug("Switching to List View")
	if err := page.Locator("#list-view-button").Click(); err != nil {
		return nil, fmt.Errorf("could not click list view button: %w", err)
	}

	// Ensure "Show available locations only" is checked
	log.Debug("Verifying availability filter")
	availableCheckbox := page.Locator("#show-available-locations-only-checkbox-input")
	isChecked, err := availableCheckbox.IsChecked()
	if err != nil {
		log.Warn("Could not determine if availability checkbox is checked", slog.String("error", err.Error()))
	} else if !isChecked {
		log.Info("Enabling availability filter")
		if err := availableCheckbox.Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)}); err != nil {
			log.Error("Could not click availability checkbox", slog.String("error", err.Error()))
		}
	}

	// Wait for results to update
	time.Sleep(3 * time.Second)

	// Store results in a map: campground name -> list of campsite names
	results := make(map[string][]string)

	// Use a more robust way to iterate over campgrounds
	// We'll find all campground click targets (buttons)
	campgroundButtons := page.Locator("button.map-link-button")
	count, err := campgroundButtons.Count()
	if err != nil {
		return nil, fmt.Errorf("could not count campground buttons: %w", err)
	}

	log.Info("Found available campgrounds", slog.Int("count", count))

	for i := 0; i < count; i++ {
		// Re-locate everything because the DOM changes
		btn := page.Locator("button.map-link-button").Nth(i)

		// Try to find the h3 name associated with this button
		// Usually it's in a parent or sibling container
		name, err := btn.Locator("xpath=..//h3").InnerText()
		if err != nil {
			// Fallback: try to find h3 in the whole list item if we can find the parent
			name = fmt.Sprintf("Campground %d", i+1)
		}

		log.Debug("Drilling down into campground", slog.String("name", name))

		if err := btn.Click(); err != nil {
			log.Error("Error clicking campground", slog.String("name", name), slog.String("error", err.Error()))
			continue
		}

		// Wait 3 seconds for campsites to load
		time.Sleep(3 * time.Second)

		// Find campsites - user says they are div[role="listitem"] but not buttons
		campsiteElements := page.Locator("div[role=\"listitem\"]")
		campsiteCount, _ := campsiteElements.Count()

		var campsiteNames []string
		for j := 0; j < campsiteCount; j++ {
			cName, err := campsiteElements.Nth(j).Locator("h3").InnerText()
			if err == nil && cName != "" {
				campsiteNames = append(campsiteNames, cName)
			}
		}
		results[name] = campsiteNames

		// Print the count as per requirement
		countStr := fmt.Sprintf("%d", len(campsiteNames))
		if len(campsiteNames) > 9 {
			countStr = "9+"
		}
		log.Info("Campground results extracted", slog.String("campground", name), slog.String("available_count", countStr))

		// Go back to the campground list
		if err := page.Locator("#map-back-button").Click(); err != nil {
			log.Warn("Could not click back button", slog.String("error", err.Error()))
			// If we can't go back, we might be stuck. Try to re-navigate or return.
			return nil, fmt.Errorf("failed to go back from campground view")
		}

		// Wait for the campground list to load again
		time.Sleep(1 * time.Second)
	}

	// Clean all keys and values in results
	finalResults := make(map[string][]string)
	for campground, campsites := range results {
		cleanCampground := strings.ReplaceAll(campground, "\n", " ")
		var cleanCampsites []string
		for _, campsite := range campsites {
			cleanCampsites = append(cleanCampsites, strings.ReplaceAll(campsite, "\n", " "))
		}
		finalResults[cleanCampground] = cleanCampsites
	}

	return finalResults, nil
}
