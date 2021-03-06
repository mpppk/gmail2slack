package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"strings"

	"bytes"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
)

type SlackWebhookContent struct {
	Text     string `json:"text"`
	Username string `json:"username"`
}

// getClient uses a Context and Config to retrieve a Token
// then generate a Client. It returns the generated Client.
func getClient(ctx context.Context, config *oauth2.Config) *http.Client {
	cacheFile, err := tokenCacheFile()
	if err != nil {
		log.Fatalf("Unable to get path to cached credential file. %v", err)
	}
	tok, err := tokenFromFile(cacheFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(cacheFile, tok)
	}
	return config.Client(ctx, tok)
}

// getTokenFromWeb uses Config to request a Token.
// It returns the retrieved Token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var code string
	if _, err := fmt.Scan(&code); err != nil {
		log.Fatalf("Unable to read authorization code %v", err)
	}

	tok, err := config.Exchange(oauth2.NoContext, code)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web %v", err)
	}
	return tok
}

// tokenCacheFile generates credential file path/filename.
// It returns the generated credential path/filename.
func tokenCacheFile() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", err
	}
	tokenCacheDir := filepath.Join(usr.HomeDir, ".credentials")
	os.MkdirAll(tokenCacheDir, 0700)
	return filepath.Join(tokenCacheDir,
		url.QueryEscape("gmail-go-quickstart.json")), err
}

// tokenFromFile retrieves a Token from a given file path.
// It returns the retrieved Token and any read error encountered.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	t := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(t)
	defer f.Close()
	return t, err
}

// saveToken uses a file path to create a file and store the
// token in it.
func saveToken(file string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", file)
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

type MessageOpt struct {
	Key   string
	Value string
}

func (m *MessageOpt) Get() (string, string) {
	return m.Key, m.Value
}

func main() {
	ctx := context.Background()

	b, err := ioutil.ReadFile("client_id.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// If modifying these scopes, delete your previously saved credentials
	// at ~/.credentials/gmail-go-quickstart.json
	config, err := google.ConfigFromJSON(b, gmail.GmailReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(ctx, config)

	srv, err := gmail.New(client)
	if err != nil {
		log.Fatalf("Unable to retrieve gmail Client %v", err)
	}

	var fromTime time.Time
	_, statErr := os.Stat("time.txt")
	if statErr == nil {
		content, err := ioutil.ReadFile("time.txt")
		if err != nil {
			panic(err)
		}
		fromTime, err = time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", string(content))
		if err != nil {
			panic(err)
		}
	} else {
		fromTime = time.Now().Add(-time.Duration(200) * time.Hour)
	}

	newFromTime := fromTime
	userName := "me"
	mr, err := srv.Users.Messages.List(userName).Do(
		&MessageOpt{"q", os.Args[1]},
		&MessageOpt{"maxResults", "5"},
	)
	if err != nil {
		log.Fatalf("Unable to retrieve messages. %v", err)
	}

	for _, m := range mr.Messages {
		mmr, err := srv.Users.Messages.Get(userName, m.Id).Do()
		if err != nil {
			log.Fatalf("Unable to retrieve messages. %v", err)
		}

		timeDiff := fromTime.Unix()*1000 - mmr.InternalDate

		// Messages are arranged in descending order
		if timeDiff > 0 {
			break
		} else {
			if (mmr.InternalDate - newFromTime.Unix()*1000) > 0 {
				newFromTime = time.Unix(mmr.InternalDate/1000, 0)
			}
		}

		b64, err := base64.URLEncoding.DecodeString(mmr.Payload.Body.Data)
		if err != nil {
			fmt.Printf("Error0: %v\n", err)
		}

		message := string(b64)
		lines := strings.Split(message, "\n")
		slackMessage := ""

		if strings.Contains(message, "お荷物の受け取り日時変更のご依頼") {
			for _, line := range lines {
				if strings.Contains(line, "■お受け取りご希望日時") ||
					strings.Contains(line, "■伝票番号") {
					slackMessage += line + "\n"
				}
			}
		} else if strings.Contains(message, "お荷物のお届けについてお知らせします。") {
			arrivalDateLinesNum := 4
			arrivalDateLinesCount := 0
			for _, line := range lines {
				if strings.Contains(line, "■お届け予定日時") {
					arrivalDateLinesCount++
					slackMessage += line + "\n"
				} else if arrivalDateLinesCount > 0 && arrivalDateLinesCount < arrivalDateLinesNum {
					slackMessage += line + "\n"
					arrivalDateLinesCount++
				}
			}
		}
		if slackMessage != "" {
			fmt.Println(slackMessage)
			content, err := json.Marshal(SlackWebhookContent{Text: slackMessage, Username: "YAMATO"})
			if err != nil {
				panic(err)
			}

			_, err = http.Post("https://hooks.slack.com/services/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", "application/json", bytes.NewReader(content))
			if err != nil {
				panic(err)
			}

		}

	}
	ioutil.WriteFile("time.txt", []byte(newFromTime.String()), 0777)
}
