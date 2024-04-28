package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/monitor/query/azmetrics"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// MockOpenIDConfiguration is a mock OpenID configuration response
	//language=JSON
	MockOpenIDConfiguration = `{
	  "authorization_endpoint":"https://login.microsoftonline.com/mock/oauth2/v2.0/authorize",
	  "issuer":"https://login.microsoftonline.com/{tenantid}/v2.0",
	  "jwks_uri":"https://login.microsoftonline.com/mock/discovery/v2.0/keys",
	  "token_endpoint":"https://login.microsoftonline.com/mock/oauth2/v2.0/token"
	}`

	// MockTokenResponse is a mock token response
	//language=JSON
	MockTokenResponse = `{
	  "access_token": "mock_access_token",
	  "expires_in": 3599,
	  "ext_expires_in": 3599,
	  "scope": "https://management.core.windows.net//.default https://metrics.monitor.azure.com/.default",
	  "token_type": "Bearer"
	}`
)

func MockTransport(next http.RoundTripper, resourceGraphResponse armresourcegraph.QueryResponse, metricsResponse azmetrics.MetricResults) promhttp.RoundTripperFunc {
	return func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "login.microsoftonline.com":
			recorder := httptest.NewRecorder()
			recorder.WriteHeader(http.StatusOK)

			switch req.URL.Path {
			case "/mock/v2.0/.well-known/openid-configuration":
				_, _ = recorder.Write([]byte(MockOpenIDConfiguration))
			case "/mock/oauth2/v2.0/token":
				_, _ = recorder.Write([]byte(MockTokenResponse))
			}

			return recorder.Result(), nil
		case "management.azure.com":
			switch req.URL.Path {
			case "/providers/Microsoft.ResourceGraph/resources":
				recorder := httptest.NewRecorder()
				recorder.WriteHeader(http.StatusOK)

				resp, err := json.Marshal(resourceGraphResponse)
				if err != nil {
					return nil, err
				}

				_, _ = recorder.Write(resp)

				return recorder.Result(), nil
			}
		default:
			if strings.HasSuffix(req.Host, "metrics.monitor.azure.com") {
				recorder := httptest.NewRecorder()
				recorder.WriteHeader(http.StatusOK)

				resp, err := json.Marshal(metricsResponse)
				if err != nil {
					return nil, err
				}

				_, _ = recorder.Write(resp)

				return recorder.Result(), nil
			}
		}

		return next.RoundTrip(req)
	}
}
