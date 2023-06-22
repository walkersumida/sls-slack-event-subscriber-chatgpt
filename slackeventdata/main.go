package slackeventdata

type SlackEventData struct {
	Type            string `json:"type"`
	TimeStamp       string `json:"time_stamp"`
	ThreadTimeStamp string `json:"thread_time_stamp"`
	Channel         string `json:"channel"`
	User            string `json:"user"`
	Message         string `json:"message"`
}
