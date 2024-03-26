package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
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

func main() {
	b, err := ioutil.ReadFile("./credentials/credentials.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	config, err := google.ConfigFromJSON(b, calendar.CalendarReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}

	client := getClient(config)

	srv, err := calendar.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to retrieve Calendar client: %v", err)
	}

	now := time.Now()
	year, month, day := now.Date()
	location := now.Location()

	var timeMin, timeMax time.Time

	// Determine the correct period based on the current date
	if day >= 16 {
		// If today is the 16th or later, timeMin is today, and timeMax is the 15th of the next month
		timeMin = time.Date(year, month, day, 0, 0, 0, 0, location)
		if month == 12 {
			timeMax = time.Date(year+1, time.January, 15, 23, 59, 59, 0, location)
		} else {
			timeMax = time.Date(year, month+1, 15, 23, 59, 59, 0, location)
		}
	} else {
		// If today is before the 16th, timeMin is the 16th of last month, and timeMax is today
		if month == 1 {
			timeMin = time.Date(year-1, time.December, 16, 0, 0, 0, 0, location)
		} else {
			timeMin = time.Date(year, month-1, 16, 0, 0, 0, 0, location)
		}
		timeMax = time.Date(year, month, day, 23, 59, 59, 0, location)
	}

	fmt.Println("Checking 'Payment' events from", timeMin.Format("2006-01-02"), "to", timeMax.Format("2006-01-02"))

	var pageToken string
	for {
		events, err := srv.Events.List("primary").ShowDeleted(false).
			SingleEvents(true).TimeMin(timeMin.Format(time.RFC3339)).TimeMax(timeMax.Format(time.RFC3339)).
			OrderBy("startTime").Q("Payment").PageToken(pageToken).Do()
		if err != nil {
			log.Fatalf("Unable to retrieve events: %v", err)
		}

		if len(events.Items) == 0 {
			fmt.Println("No upcoming 'Payment' events found.")
			break
		}

		for _, item := range events.Items {
			if item.Start.Date != "" && strings.HasPrefix(item.Summary, "Payment") {
				fmt.Printf("%v - %v\n", item.Start.Date, item.Summary)
			}
		}

		pageToken = events.NextPageToken
		if pageToken == "" {
			break // No more pages to fetch
		}
	}
}
