package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSiteInfoPublishesConfiguredSignInIDsAndPresentationOnly(t *testing.T) {
	s, _, _ := setup(t)
	s.Config.OIDCissuer = "https://issuer.test"
	s.Config.OIDCclientID = "client"
	s.Config.OIDCsecret = "private-oidc-secret"
	s.Config.OIDCid = "campus"
	s.Config.OIDCname = "校园账号"
	s.Config.OIDCiconURL = "https://cdn.test/campus.svg"
	s.Config.QQappID = "app"
	s.Config.QQsecret = "private-qq-secret"
	recorder := httptest.NewRecorder()
	s.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/site/info", nil))
	var response struct {
		Data struct {
			Providers []string `json:"providers"`
			Details   []struct {
				ID, Kind, Name string
				IconURL        string `json:"icon_url"`
			} `json:"provider_details"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Providers) != 2 || response.Data.Providers[0] != "campus" || response.Data.Providers[1] != "qq" {
		t.Fatal(response.Data.Providers)
	}
	if len(response.Data.Details) != 2 || response.Data.Details[0].Name != "校园账号" || response.Data.Details[0].Kind != "oidc" || response.Data.Details[0].IconURL != "https://cdn.test/campus.svg" {
		t.Fatal(response.Data.Details)
	}
	if strings.Contains(recorder.Body.String(), "private-") {
		t.Fatal("provider secret leaked")
	}
}
