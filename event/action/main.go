package main

import (
	"context"
	"log"
	"os"
	"regexp"

	l "github.com/aws/aws-lambda-go/lambda"
	"github.com/go-resty/resty/v2"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

func Handler(ctx context.Context, input *slackevents.AppMentionEvent) {
	cli := slack.New(os.Getenv("SLACK_ACCESS_TOKEN"))
	text := textWithMentionsRemoved(input.Text)

	cID, timestamp, err := cli.PostMessageContext(
		ctx,
		input.Channel,
		slack.MsgOptionText(
			"...", false,
		),
	)
	if err != nil {
		log.Printf("failed to post a message: %s", err)
	}

	client := resty.New()
	resp, err := chatgpt(client, text)
	if err != nil {
		log.Printf("failed to call chatgpt: %s", err)
		return
	}
	result := resp.Result().(*Response)

	_, _, _, err = cli.UpdateMessageContext(
		ctx,
		cID,
		timestamp,
		slack.MsgOptionText(
			result.Choices[0].Message.Content, false,
		),
	)
	if err != nil {
		log.Printf("failed to update a message: %s", err)
	}
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
