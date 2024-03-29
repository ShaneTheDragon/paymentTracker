package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

var (
	TotalRemainingOn = "Last Day of the Month" // Options: "Last Day of the Month", "First Day of the Month", "Pay Date"
	TimeZone         = "Asia/Karachi"          // Default time zone
	PayDate          = 28
)

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config) *http.Client {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your web browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	fmt.Println("Enter the authorization code here: ")
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.Background(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

// parseAmountFromSummary attempts to find and parse an amount from an event summary.
func parseAmountFromSummary(summary string) (float64, bool) {
	// Regex to find an amount in the format "£999,000" or "£999,000.00", with or without the £ and comma, and with optional decimal places.
	re := regexp.MustCompile(`£?(\d{1,3}(,\d{3})*|\d+)(\.\d{1,2})?`)
	matches := re.FindStringSubmatch(summary)
	if len(matches) == 0 {
		return 0, false // No match found
	}
	amountStr := matches[0]
	// Remove £ and comma for parsing
	amountStr = strings.ReplaceAll(amountStr, "£", "")
	amountStr = strings.ReplaceAll(amountStr, ",", "")
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		return 0, false // Failed to parse amount
	}
	return amount, true
}

// calculateTotalPayments goes through event items and sums up all payment amounts.
func calculateTotalPayments(srv *calendar.Service, startDate, endDate time.Time) float64 {
	var total float64
	now := time.Now() // Get current time to compare with event dates

	// Ensure start date is not before today
	if startDate.Before(now) {
		startDate = now
	}

	events, err := srv.Events.List("primary").
		ShowDeleted(false).
		SingleEvents(true).
		TimeMin(startDate.Format(time.RFC3339)).
		TimeMax(endDate.Format(time.RFC3339)).
		OrderBy("startTime").
		Q("Payment").
		Do()
	if err != nil {
		log.Fatalf("Unable to retrieve payment events: %v", err)
	}

	for _, item := range events.Items {
		if amount, ok := parseAmountFromSummary(item.Summary); ok {
			total += amount
		}
	}

	return total
}

func manageTotalRemainingEvent(srv *calendar.Service, total float64) error {
	loc, err := time.LoadLocation(TimeZone)
	if err != nil {
		log.Fatalf("Failed to load time zone '%s': %v", TimeZone, err)
	}
	now := time.Now().In(loc)

	var eventDate time.Time

	switch TotalRemainingOn {
	case "Last Day of the Month":
		firstOfNextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, loc)
		eventDate = firstOfNextMonth.Add(-24 * time.Hour)
	case "First Day of the Month":
		eventDate = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	case "Pay Date":
		// Assuming PayDate is a global variable indicating the day of the month the pay occurs
		eventDate = time.Date(now.Year(), now.Month(), PayDate, 0, 0, 0, 0, loc)
		if now.Day() > PayDate {
			// If today is after the pay date, set the event for the pay date of the next month
			eventDate = eventDate.AddDate(0, 1, 0)
		}
	default:
		log.Fatalf("Invalid TotalRemainingOn value: %v", TotalRemainingOn)
	}

	// Delete existing "Total Remaining" events
	events, err := srv.Events.List("primary").
		ShowDeleted(false).
		SingleEvents(true).
		Q("Total Remaining").Do()

	if err != nil {
		log.Fatalf("Failed to retrieve events: %v", err)
	}

	for _, item := range events.Items {
		if strings.HasPrefix(item.Summary, "Total Remaining") {
			err := srv.Events.Delete("primary", item.Id).Do()
			if err != nil {
				return fmt.Errorf("unable to delete event: %v", err)
			}
		}
	}

	// Create the new "Total Remaining" event based on eventDate and TimeZone
	event := &calendar.Event{
		Summary: fmt.Sprintf("Total Remaining £%.2f", total),
		Start: &calendar.EventDateTime{
			Date:     eventDate.Format("2006-01-02"),
			TimeZone: TimeZone,
		},
		End: &calendar.EventDateTime{
			Date:     eventDate.AddDate(0, 0, 1).Format("2006-01-02"),
			TimeZone: TimeZone,
		},
		ColorId: "11", // Assuming "11" is red; adjust based on your calendar settings
	}

	_, err = srv.Events.Insert("primary", event).Do()
	if err != nil {
		return fmt.Errorf("unable to create event: %v", err)
	}

	return nil
}

// Generates future "Total Remaining" events for the next 11 months
func generateFutureTotalRemainingEvents(srv *calendar.Service, loc *time.Location) {
	now := time.Now().In(loc)

	for i := 1; i <= 11; i++ {
		futureMonth := now.AddDate(0, i, 0)
		year, month := futureMonth.Year(), futureMonth.Month()

		startDate, endDate := getPaymentPeriodDates(year, int(month), loc)
		total := calculateTotalPayments(srv, startDate, endDate)
		if err := manageTotalRemainingEventForMonth(srv, total, year, month, loc); err != nil {
			log.Fatalf("Error managing the 'Total Remaining' event for %v %d: %v", month, year, err)
		}
	}
}

// Helper function to calculate the start and end dates for payment calculations
func getPaymentPeriodDates(year, month int, loc *time.Location) (startDate, endDate time.Time) {
	startDate = time.Date(year, time.Month(month), PayDate, 0, 0, 0, 0, loc)
	if month == 12 {
		endDate = time.Date(year+1, time.Month(1), PayDate, 0, 0, 0, 0, loc).Add(-time.Second)
	} else {
		endDate = time.Date(year, time.Month(month+1), PayDate, 0, 0, 0, 0, loc).Add(-time.Second)
	}
	return
}

func manageTotalRemainingEventForMonth(srv *calendar.Service, total float64, year int, month time.Month, loc *time.Location) error {
	var eventDate time.Time

	switch TotalRemainingOn {
	case "Last Day of the Month":
		// Calculate the last day of the given month
		firstOfNextMonth := time.Date(year, month+1, 1, 0, 0, 0, 0, loc)
		lastDayOfMonth := firstOfNextMonth.Add(-24 * time.Hour)
		eventDate = lastDayOfMonth
	case "First Day of the Month":
		eventDate = time.Date(year, month, 1, 0, 0, 0, 0, loc)
	case "Pay Date":
		// For "Pay Date", you might want to adjust based on your business logic.
		// The below code will set the event for the PayDate of the given month
		eventDate = time.Date(year, month, PayDate, 0, 0, 0, 0, loc)
	default:
		return fmt.Errorf("invalid TotalRemainingOn value: %v", TotalRemainingOn)
	}

	// Create and insert the event as done in manageTotalRemainingEvent
	event := &calendar.Event{
		Summary: fmt.Sprintf("Total Remaining £%.2f", total),
		Start: &calendar.EventDateTime{
			Date:     eventDate.Format("2006-01-02"),
			TimeZone: loc.String(),
		},
		End: &calendar.EventDateTime{
			Date:     eventDate.AddDate(0, 0, 1).Format("2006-01-02"),
			TimeZone: loc.String(),
		},
		ColorId: "11", // Assuming "11" represents the color red
	}

	_, err := srv.Events.Insert("primary", event).Do()
	if err != nil {
		return fmt.Errorf("unable to create event: %v", err)
	}

	return nil
}

func main() {
	b, err := ioutil.ReadFile("./credentials/credentials.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// Deserialize the credentials and initialize the config
	config, err := google.ConfigFromJSON(b, calendar.CalendarScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}

	// Now that 'config' is defined, you can use it to get a client
	client := getClient(config)

	srv, err := calendar.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to retrieve Calendar client: %v", err)
	}

	loc, err := time.LoadLocation(TimeZone)
	if err != nil {
		log.Fatalf("Failed to load time zone '%s': %v", TimeZone, err)
	}
	now := time.Now().In(loc)

	// Calculate the start and end dates for the payment calculation
	var startDate, endDate time.Time
	if now.Day() >= PayDate {
		startDate = time.Date(now.Year(), now.Month(), PayDate, 0, 0, 0, 0, loc)
		endDate = startDate.AddDate(0, 1, 0).Add(-time.Second)
	} else {
		startDate = time.Date(now.Year(), now.Month()-1, PayDate, 0, 0, 0, 0, loc)
		endDate = startDate.AddDate(0, 1, 0).Add(-time.Second)
	}

	total := calculateTotalPayments(srv, startDate, endDate)

	if err := manageTotalRemainingEvent(srv, total); err != nil {
		log.Fatalf("Error managing the 'Total Remaining' event: %v", err)
	}

	// Generate future "Total Remaining" events
	generateFutureTotalRemainingEvents(srv, loc)
}
