package immobilienscout24

import (
	"net/http"

	"github.com/dghubble/oauth1"
)

type Client struct {
	httpClient *http.Client
}

func NewClient(consumerKey, consumerSecret, accessToken, accessTokenSecret string) *Client {
	config := oauth1.NewConfig(consumerKey, consumerSecret)
	token := oauth1.NewToken(accessToken, accessTokenSecret)
	httpClient := config.Client(oauth1.NoContext, token)
	return &Client{httpClient: httpClient}
}
