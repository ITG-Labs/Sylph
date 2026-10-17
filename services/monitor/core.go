package monitor

import (
	"context"
	"net/http"

	"github.com/ITG-Labs/sylph/pkg/apiclient"
)

type CoreClient struct {
	api *apiclient.Client
}

func NewCoreClient(api *apiclient.Client) *CoreClient {
	return &CoreClient{api: api}
}

func (c *CoreClient) State(ctx context.Context) (*State, error) {
	var st State
	if err := c.api.Do(ctx, http.MethodGet, "/v1/monitor/state", nil, &st); err != nil {
		return nil, err
	}
	return &st, nil
}
