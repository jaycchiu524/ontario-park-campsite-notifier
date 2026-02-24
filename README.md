# Ontario Parks Campsite Notifier

A robust web scraper built with Go and Playwright to monitor campsite availability at Ontario Parks.

## Features

- **Parallel Monitoring**: Check multiple parks and dates simultaneously using Go routines.
- **YAML Configuration**: Easily manage search targets, including park names, arrival dates, and duration.
- **Smart Park Expansion**: Use prefixes (e.g., "Algonquin") to automatically monitor all sub-parks in that group.
- **Structured Logging**: Uses `log/slog` for professional observability and debugging (JSON or Text format).
- **Automated Reporting**: Generates a clean `report.yaml` summarizing available campsites across all monitored targets.
- **Stealth Integration**: Uses playwright-stealth to minimize detection by automated bot counters.

## Getting Started

### Prerequisites

- [Go](https://golang.org/doc/install) (1.21+)
- [Playwright for Go](https://playwright.dev/go/docs/intro)

### Installation

1. Clone the repository
2. Install dependencies:
   ```bash
   go mod download
   ```
3. Install Playwright browsers:
   ```bash
   go run github.com/playwright-community/playwright-go/cmd/playwright@latest install --with-deps chromium
   ```

### Configuration

#### `.env` File

Create a `.env` file in the root directory:

```env
# Monitoring interval in minutes
CHECK_INTERVAL_MINUTES=10

# Development mode (headless: false, text logging)
DEV=true

# Custom filenames (optional)
TARGETS_FILE="targets.yaml"
REPORT_FILE="report.yaml"
```

#### `targets.yaml`

Define your search targets:

```yaml
targets:
  - park: "Algonquin" # Matches "Algonquin - Mew Lake", "Algonquin - Tea Lake", etc.
    arrival: "2026-03-15"
    nights: 2
    equipment: "Single Tent"
  - park: "Killbear"
    arrival: "2026-03-10"
    nights: 2
    equipment: "Single Tent"
```

### Running the Monitor

```bash
go run cmd/monitor/main.go
```

## Observability

The application uses structured logging. In production (`DEV=false`), logs are output in JSON format for easy ingestion by log management tools. In development (`DEV=true`), logs are output in a human-readable text format.

Key events logged:

- Monitoring initialization and target expansion.
- Detailed steps for each parallel search.
- Summary of campground and campsite findings.
- Report generation status.

## Output

- **Logs**: Real-time progress and error reporting in the terminal.
- **report.yaml**: A structured summary of all available campsites found during the latest check.
