package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"

	l "github.com/aws/aws-lambda-go/lambda"
	"github.com/go-resty/resty/v2"
	slackgo "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

func Handler(ctx context.Context, input *slackevents.AppMentionEvent) error {
	slack := NewSlack()
	text := textWithMentionsRemoved(input.Text)

	cID, timestamp, err := slack.cli.PostMessageContext(
		ctx,
		input.Channel,
		slackgo.MsgOptionText(
			"...", false,
		),
		slackgo.MsgOptionTS(input.TimeStamp),
	)
	if err != nil {
		log.Printf("failed to post a message: %s", err)
		return err
	}

	client := resty.New()
	resp, err := chatgpt(client, text)
	if err != nil {
		log.Printf("failed to call chatgpt: %s", err)
		return err
	}
	result := resp.Result().(*Response)

	if resp.IsError() {
		log.Printf("error: status=%d, response=%+v", resp.StatusCode(), resp.RawResponse)

		msg := fmt.Sprintf("... (http status: %d)", resp.StatusCode())
		err = slack.updateMessage(ctx, cID, timestamp, msg, input.User)
		if err != nil {
			log.Printf("failed to update a message: %s", err)
			return err
		}

		return err
	}

	err = slack.updateMessage(ctx, cID, timestamp, result.Choices[0].Message.Content, input.User)
	if err != nil {
		log.Printf("failed to update a message: %s", err)
		return err
	}

	return nil
}

type Slack struct {
	cli *slackgo.Client
}

func NewSlack() *Slack {
	return &Slack{
		cli: slackgo.New(os.Getenv("SLACK_ACCESS_TOKEN")),
	}
}

func (s *Slack) updateMessage(ctx context.Context, cID, timestamp, msg, userID string) error {
	if userID != "" {
		msg = fmt.Sprintf("<@%s> %s", userID, msg)
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

func textWithMentionsRemoved(txt string) string {
	re := regexp.MustCompile("<@.+?>")
	return re.ReplaceAllString(txt, "")
}

type RequestBody struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Response struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int      `json:"created"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func chatgpt(client *resty.Client, text string) (*resty.Response, error) {
	resp, err := client.
		SetHeader("Authorization", "Bearer "+os.Getenv("API_KEY")).
		SetHeader("Content-Type", "application/json").
		R().
		SetBody(
			RequestBody{
				Model:    "gpt-3.5-turbo",
				Messages: []Message{{Role: "user", Content: text}},
			},
		).
		SetResult(&Response{}).
		Post("https://api.openai.com/v1/chat/completions")
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func main() {
	l.Start(Handler)
}
