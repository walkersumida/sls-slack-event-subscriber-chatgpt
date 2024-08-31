package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	l "github.com/aws/aws-lambda-go/lambda"
	slackgo "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/walkersumida/sls-slack-event-subscriber-chatgpt/slackeventdata"
)

const KB = 1024

func Handler(ctx context.Context, input slackeventdata.SlackEventData) error {
	if input.Type == string(slackevents.Message) && !isBotMentioned(input.Message) {
		return nil
	}

	slack := NewSlack()

	timeStamp := input.TimeStamp
	threadTimeStamp := input.ThreadTimeStamp
	channel := input.Channel
	user := input.User

	ts := timeStamp
	if isThread(threadTimeStamp) {
		ts = threadTimeStamp
	}
	slackMsgs, _, _, err := slack.cli.GetConversationRepliesContext(ctx, &slackgo.GetConversationRepliesParameters{
		ChannelID: channel,
		Timestamp: ts,
	})
	if err != nil {
		log.Printf("failed to get conversation replies: %s", err)
		return err
	}

	msgs := buildMessage(user, slackMsgs)
	resp, err := chatgpt(msgs)
	if err != nil {
		log.Printf("failed to call chatgpt: %s", err)
		return err
	}

	cID, timestamp, err := slack.postMessage(ctx, channel, "...", timeStamp, user)
	if err != nil {
		log.Printf("failed to post a message: %s", err)
		return err
	}

	now := time.Now()
	message := ""
	for {
		data := make([]byte, 5*KB)
		_, err := resp.Body.Read(data)
		if err != nil {
			return err
		}

		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if line == "data: [DONE]" {
				if err := slack.updateMessage(ctx, cID, timestamp, message, user); err != nil {
					log.Printf("failed to update a message: %s", err)
					return err
				}

				return nil
			}
			re := regexp.MustCompile(`^data: `)
			if !re.MatchString(line) {
				continue
			}

			var r Response
			if err := json.Unmarshal([]byte(line[5:]), &r); err != nil {
				log.Println(err)
				if e, ok := err.(*json.SyntaxError); ok {
					log.Printf("syntax error at byte offset %d", e.Offset)
				}
				continue
			}
			if r.Choices[0].Delta == nil || r.Choices[0].Delta.Content == nil {
				continue
			}

			message += *r.Choices[0].Delta.Content
		}

		if time.Since(now) > 3*time.Second {
			if err := slack.updateMessage(ctx, cID, timestamp, message, ""); err != nil {
				log.Printf("failed to update a message: %s", err)
				return err
			}
			now = time.Now()
		}
	}
}

func isThread(ts string) bool {
	return ts != ""
}

type Slack struct {
	cli *slackgo.Client
}

func NewSlack() *Slack {
	return &Slack{
		cli: slackgo.New(os.Getenv("SLACK_ACCESS_TOKEN")),
	}
}

func (s *Slack) postMessage(ctx context.Context, cID, msg, timeStamp, userID string) (string, string, error) {
	_, timestamp, err := s.cli.PostMessageContext(
		ctx,
		cID,
		slackgo.MsgOptionText(msg, false),
		slackgo.MsgOptionTS(timeStamp),
	)
	if err != nil {
		return "", "", err
	}

	return cID, timestamp, nil
}

func (s *Slack) updateMessage(ctx context.Context, cID, timestamp, msg, userID string) error {
	if userID != "" {
		msg = fmt.Sprintf("%s\n<@%s>", msg, userID)
	}

	_, _, _, err := s.cli.UpdateMessageContext(
		ctx,
		cID,
		timestamp,
		slackgo.MsgOptionText(msg, false),
	)
	if err != nil {
		return err
	}
	return nil
}

func textWithMentionsRemoved(txt *string) *string {
	re := regexp.MustCompile("<@.+?>")
	replaced := re.ReplaceAllString(*txt, "")
	return &replaced
}

type RequestBody struct {
	Stream   bool      `json:"stream"`
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type Message struct {
	Role    *string `json:"role"`
	Content *string `json:"content"`
}

type Response struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int      `json:"created"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int      `json:"index"`
	Delta        *Message `json:"delta"`
	Message      *Message `json:"message"`
	FinishReason string   `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func buildMessage(user string, msgs []slackgo.Message) []Message {
	msgsBody := []Message{}
	for _, msg := range msgs {
		role := "user"
		if isBot(msg.User) {
			role = "assistant"
		}
		if role == "user" {
			if !isBotMentioned(msg.Text) {
				continue
			}
		}
		msgsBody = append(msgsBody, Message{
			Role:    &role,
			Content: textWithMentionsRemoved(&msg.Text),
		})
	}

	return msgsBody
}

func chatgpt(msgs []Message) (*http.Response, error) {
	values := RequestBody{
		Stream:   true,
		Model:    os.Getenv("MODEL"),
		Messages: msgs,
	}
	jsonValue, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(
		"POST",
		"https://api.openai.com/v1/chat/completions",
		bytes.NewBuffer(jsonValue),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+os.Getenv("API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func isBot(id string) bool {
	return id == os.Getenv("SLACK_BOT_USER_ID")
}

func isBotMentioned(msg string) bool {
	re := regexp.MustCompile(fmt.Sprintf("<@%s>", os.Getenv("SLACK_BOT_USER_ID")))
	return re.MatchString(msg)
}

func main() {
	l.Start(Handler)
}
