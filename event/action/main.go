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

	ts := input.TimeStamp
	if isThread(input.ThreadTimeStamp) {
		ts = input.ThreadTimeStamp
	}
	slackMsgs, _, _, err := slack.cli.GetConversationRepliesContext(ctx, &slackgo.GetConversationRepliesParameters{
		ChannelID: input.Channel,
		Timestamp: ts,
	})
	if err != nil {
		log.Printf("failed to get conversation replies: %s", err)
		return err
	}

	client := resty.New()
	msgs := buildMessage(input.User, slackMsgs)
	resp, err := chatgpt(client, msgs)
	if err != nil {
		log.Printf("failed to call chatgpt: %s", err)
		return err
	}
	result := resp.Result().(*Response)

	if resp.IsError() {
		log.Printf("error: status=%d, response=%+v", resp.StatusCode(), resp.RawResponse)

		msg := fmt.Sprintf("... (http status: %d)", resp.StatusCode())
		err = slack.postMessage(ctx, input.Channel, msg, input.TimeStamp, input.User)
		if err != nil {
			log.Printf("failed to update a message: %s", err)
			return err
		}

		return err
	}

	err = slack.postMessage(ctx, input.Channel, result.Choices[0].Message.Content, input.TimeStamp, input.User)
	if err != nil {
		log.Printf("failed to post a message: %s", err)
		return err
	}

	return nil
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

func (s *Slack) postMessage(ctx context.Context, cID, msg, timeStamp, userID string) error {
	if userID != "" {
		msg = fmt.Sprintf("<@%s> %s", userID, msg)
	}

	_, _, err := s.cli.PostMessageContext(
		ctx,
		cID,
		slackgo.MsgOptionText(msg, false),
		slackgo.MsgOptionTS(timeStamp),
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
			Role:    role,
			Content: textWithMentionsRemoved(msg.Text),
		})
	}
	log.Printf("built messages: %+v", msgsBody)
	return msgsBody
}

func chatgpt(client *resty.Client, msgs []Message) (*resty.Response, error) {
	resp, err := client.
		SetHeader("Authorization", "Bearer "+os.Getenv("API_KEY")).
		SetHeader("Content-Type", "application/json").
		R().
		SetBody(
			RequestBody{
				Model:    "gpt-3.5-turbo",
				Messages: msgs,
			},
		).
		SetResult(&Response{}).
		Post("https://api.openai.com/v1/chat/completions")
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
