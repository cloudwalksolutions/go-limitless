package fixture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/theboarderline/go-limitless/src/pkg/common"
	"github.com/theboarderline/go-limitless/src/server/auth"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/antchfx/jsonquery"
	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/jinzhu/now"
	"github.com/joho/godotenv"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var defaultOpts = godog.Options{
	Paths:     []string{"features"},
	Output:    colors.Colored(os.Stdout),
	Randomize: time.Now().UTC().UnixNano(),
	Format:    "pretty",
}

func NewServerFixture(opts *godog.Options) *ServerFeature {
	if opts == nil {
		opts = &defaultOpts
	}
	godog.BindCommandLineFlags("godog.", opts)

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	_ = godotenv.Load(".env")

	viper.SetDefault("lifecycle", "local")
	viper.SetDefault("http_scheme", "https")

	if err := viper.ReadInConfig(); err != nil {
		if errors.As(err, &viper.ConfigFileNotFoundError{}) {
			log.Warn().Msg("Config file not found; ignore error if desired")
		} else {
			log.Warn().Err(err).Msg("Config file was found but another error was produced")
		}
	}

	pflag.BoolP("debug", "v", viper.GetBool("debug"), "debug logs enabled")
	pflag.StringP("lifecycle", "l", viper.GetString("lifecycle"), "lifecycle to run tests against")
	pflag.Parse()
	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		log.Fatal().Err(err).Msg("failed to bind flags")
	}

	if viper.GetBool("debug") {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	return &ServerFeature{
		replacements: make(map[string]interface{}),
		store:        make(map[string]interface{}),
	}
}

type ServerFeature struct {
	replacements map[string]interface{}
	store        map[string]interface{}

	client *http.Client

	httpResponse *http.Response
	responseBody string

	response     common.Response
	authResponse auth.Response

	user auth.User
}

func (s *ServerFeature) reset(interface{}) {
	s.replacements = make(map[string]interface{})
	s.store = make(map[string]interface{})

	s.httpResponse = nil
	s.responseBody = ""

	s.response = common.Response{}
	s.authResponse = auth.Response{}

	s.user = auth.User{}
}

func init() {
}

func (s *ServerFeature) Run(m *testing.M) {
	RegisterFailHandler(func(message string, _ ...int) {
		panic(message)
	})

	if err := godotenv.Load(".env"); err != nil {
		log.Warn().Err(err).Msg("failed to load .env file")
	}

	status := godog.TestSuite{
		ScenarioInitializer: InitializeScenario,
		Options:             &defaultOpts,
	}.Run()

	os.Exit(status)
}

func (s *ServerFeature) SendRequestWithData(method, endpoint string, body *godog.DocString) error {
	req, err := http.NewRequest(method, endpoint, s.PrepareBody(body.Content))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	return s.Do(req)
}

func (s *ServerFeature) SendRequestWithParams(method, endpoint string, params *godog.DocString) error {
	req, err := http.NewRequest(method, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	paramsMap := make(map[string]interface{})
	if err = json.Unmarshal([]byte(params.Content), &paramsMap); err != nil {
		return fmt.Errorf("failed to unmarshal params: %v", err)
	}

	q := req.URL.Query()
	for k, v := range paramsMap {
		q.Add(k, fmt.Sprint(v))
	}

	req.URL.RawQuery = q.Encode()

	return s.Do(req)
}

func (s *ServerFeature) SendRequest(method, endpoint string) error {
	req, err := http.NewRequest(method, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	return s.Do(req)
}

func (s *ServerFeature) TheResponseCodeShouldBe(statusCode int) error {
	actual := s.httpResponse.StatusCode
	expected := statusCode
	if actual != expected {
		return fmt.Errorf("expected status code %d, got %d: %s", expected, actual, PrettifyJSON(s.responseBody))
	}
	return nil
}

func (s *ServerFeature) TheResponseShouldNotBeEmpty() error {
	if s.responseBody == "" {
		return fmt.Errorf("response is empty")
	}

	return nil
}

func (s *ServerFeature) TheResponseShouldContain(body *godog.DocString) error {
	actual := common.CleanString(fmt.Sprint(s.responseBody))
	expected := common.CleanString(body.Content)
	if actual == "" {
		return fmt.Errorf("response is empty")
	} else if !strings.Contains(actual, expected) {
		return fmt.Errorf("response does not contain %s, got %s", expected, PrettifyJSON(actual))
	}

	return nil
}

func (s *ServerFeature) TheResponseShouldContainA(key string) error {
	key = s.ReplaceValues(key)

	res, err := json.Marshal(s.responseBody)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %v", err)
	}

	actual := common.CleanString(string(res))
	if !strings.Contains(actual, key) {
		return fmt.Errorf("response does not contain %s, got %s", key, PrettifyJSON(actual))
	}

	return nil
}

func (s *ServerFeature) TheResponseShouldContainAWithItems(key string, body *godog.DocString) error {
	key = s.ReplaceValues(key)

	val, err := s.GetNodeFromResponse(key)
	if err != nil {
		return err
	}

	actual := val.Value()
	if actual == nil || actual == "<nil>" {
		return fmt.Errorf("item not found in response: %s", PrettifyJSON(s.responseBody))
	}

	var expectedItems []interface{}
	if err = json.Unmarshal([]byte(body.Content), &expectedItems); err != nil {
		return fmt.Errorf("expected items is not a list: %v", err)
	}

	for _, expectedItem := range expectedItems {
		if actualList, ok := actual.([]interface{}); ok {
			found := false
			for _, actualItem := range actualList {
				if reflect.DeepEqual(actualItem, expectedItem) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("response item does not contain %v, got %v", expectedItem, actual)
			}
		} else {
			return fmt.Errorf("response item is not a list")
		}
	}

	return nil
}

func (s *ServerFeature) TheResponseShouldContainSetTo(property, value string) error {
	value = s.ReplaceValues(value)

	val, err := s.GetNodeFromResponse(property)
	if err != nil {
		return err
	}

	if fmt.Sprint(val.Value()) != value {
		return fmt.Errorf("the json query path %s does not contain %s: %s", property, value, PrettifyJSON(s.responseBody))
	}

	return nil
}

func (s *ServerFeature) TheResponseShouldContainATimeSetTo(jsonQueryPath, value string) error {
	val, err := s.GetNodeFromResponse(jsonQueryPath)
	if err != nil {
		return err
	}

	actualValue := fmt.Sprint(val.Value())
	actualTime, err := now.Parse(actualValue)
	if err != nil {
		return fmt.Errorf("failed to parse actual time: %v", err)
	}
	expectedTime, err := now.Parse(value)
	if err != nil {
		return fmt.Errorf("failed to parse expected time: %v", err)
	}

	if actualTime != expectedTime {
		return fmt.Errorf("the json query path %s does not contain %s: %s", jsonQueryPath, value, PrettifyJSON(s.responseBody))
	}

	return nil
}

func (s *ServerFeature) TheResponseShouldContainAThatIsNull(jsonQueryPath string) error {
	val, err := s.GetNodeFromResponse(jsonQueryPath)
	if err != nil {
		return err
	}

	dataFound := false

	for _, child := range val.ChildNodes() {
		if child.Value() != nil && child.Value() != "<nil>" {
			dataFound = true
			break
		}
	}

	if dataFound {
		return fmt.Errorf("the json query path %s does not contain a null value: %s", jsonQueryPath, PrettifyJSON(s.responseBody))
	}

	return nil
}

func (s *ServerFeature) TheResponseShouldContainAThatIsNotNull(jsonQueryPath string) error {
	val, err := s.GetNodeFromResponse(jsonQueryPath)
	if err != nil {
		return err
	}

	dataFound := false

	for _, child := range val.ChildNodes() {
		if child.Value() != nil && child.Value() != "<nil>" {
			dataFound = true
			break
		}
	}

	if !dataFound {
		return fmt.Errorf("the json query path %s contains a null value", jsonQueryPath)
	}

	return nil
}

func (s *ServerFeature) TheResponseShouldContainAThatIsEmpty(jsonQueryPath string) error {
	val, err := s.GetNodeFromResponse(jsonQueryPath)
	if err != nil {
		return err
	}

	if len(val.ChildNodes()) > 0 {
		return fmt.Errorf("the json query path %s contains items: %s", jsonQueryPath, PrettifyJSON(s.responseBody))
	}

	return nil
}

func (s *ServerFeature) TheResponseShouldContainAThatIsNotEmpty(jsonQueryPath string) error {
	val, err := s.GetNodeFromResponse(jsonQueryPath)
	if err != nil {
		return err
	}

	if len(val.ChildNodes()) == 0 {
		return fmt.Errorf("the json query path %s does not contain any items: %s", jsonQueryPath, PrettifyJSON(s.responseBody))
	}

	return nil
}

func (s *ServerFeature) TheResponseHaveLength(length int) error {
	items := make([]interface{}, 0)
	if err := json.Unmarshal([]byte(s.responseBody), &items); err != nil {
		return fmt.Errorf("failed to unmarshal response into list: %v", err)
	}

	if len(items) != length {
		return fmt.Errorf("the response contains %d items, expected %d: %s", len(items), length, PrettifyJSON(s.responseBody))
	}

	return nil
}

func (s *ServerFeature) TheResponseShouldContainAWithLength(jsonQueryPath string, length int) error {
	val, err := s.GetNodeFromResponse(jsonQueryPath)
	if err != nil {
		return err
	}

	if len(val.ChildNodes()) != length {
		return fmt.Errorf("the json query path %s does not contain %d items: %s", jsonQueryPath, length, PrettifyJSON(s.responseBody))
	}

	return nil
}

func (s *ServerFeature) TheResponseContainsItemWithPropertySetTo(property, value string) error {
	value = s.ReplaceValues(value)

	items := make([]interface{}, 0)
	if err := json.Unmarshal([]byte(s.responseBody), &items); err != nil {
		return fmt.Errorf("failed to unmarshal response into list: %v", err)
	}

	for _, item := range items {
		itemMap := item.(map[string]interface{})
		itemValue := fmt.Sprint(itemMap[property])
		if itemValue == value {
			return nil
		}
	}

	return fmt.Errorf("no item found with %s set to %s", property, value)
}

func (s *ServerFeature) TheResponseContainsItemAtIndexWithPropertySetTo(index int, property, value string) error {
	value = s.ReplaceValues(value)

	items := make([]interface{}, 0)
	if err := json.Unmarshal([]byte(s.responseBody), &items); err != nil {
		return fmt.Errorf("failed to unmarshal response into list: %v", err)
	}

	if index >= len(items) {
		return fmt.Errorf("not enough items in response to get item at index %d, found %d", index, len(items))
	}

	itemMap := items[index].(map[string]interface{})
	itemValue := fmt.Sprint(itemMap[property])
	if itemValue != value {
		return fmt.Errorf("item at index %d does not have %s set to %s, found %s", index, property, value, itemValue)
	}

	return nil
}

func (s *ServerFeature) SaveValueFromResponse(key string) error {
	val, err := s.GetNodeFromResponse(key)
	if err != nil {
		return err
	}

	s.store[key] = val.Value()
	return nil
}

func (s *ServerFeature) SaveValueFromResponseList(index int, key, value string) error {
	val, err := s.GetNodeFromResponse(key)
	if err != nil {
		return err
	}

	if index >= len(val.ChildNodes()) {
		return fmt.Errorf("not enough items in response to get item at index %d, found %d", index, len(val.ChildNodes()))
	}

	s.store[value] = val.ChildNodes()[index].Value()
	return nil
}

func (s *ServerFeature) GetNodeFromResponse(queryPath string) (*jsonquery.Node, error) {
	doc, err := jsonquery.Parse(strings.NewReader(s.responseBody))
	if err != nil {
		return nil, err
	}

	queryPath = strings.ReplaceAll(queryPath, ".", "/")

	extractedValue := jsonquery.FindOne(doc, fmt.Sprintf("//%s", queryPath))
	if extractedValue == nil {
		return nil, fmt.Errorf("'%s' not found in response: %s", queryPath, PrettifyJSON(s.responseBody))
	}

	return extractedValue, nil
}

func (s *ServerFeature) TheResponseShouldNotContainA(key string) error {
	res, err := json.Marshal(s.responseBody)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %v", err)
	}

	actual := common.CleanString(string(res))
	if strings.Contains(actual, key) {
		return fmt.Errorf("response contains %s, got %s", key, actual)
	}

	return nil
}

func (s *ServerFeature) TheResponseShouldMatchJSON(body *godog.DocString) error {
	if s.responseBody == "" {
		return fmt.Errorf("response is empty")
	} else if !strings.Contains(s.responseBody, body.Content) {
		return fmt.Errorf("response does not match %s, got %s", body.Content, s.responseBody)
	}

	return nil
}

func (s *ServerFeature) PrepareBody(body string) io.Reader {
	replacedBody := s.ReplaceValues(body)
	return strings.NewReader(replacedBody)
}

func (s *ServerFeature) Do(req *http.Request) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}

	req.URL = s.FormatURL(req.URL.String())

	if s.authResponse.Token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.authResponse.Token))
	}

	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		replacedBody := s.PrepareBody(string(body))
		req.Body = io.NopCloser(replacedBody)
		req.Header.Set("Content-Type", "application/json")
		log.Info().Msgf("POST REQUEST BODY: %s", replacedBody)
	}

	response, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %v", err)
	}

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err)
	}

	log.Info().
		Str("response", PrettifyJSON(string(responseBody))).
		Msg("HTTP RESPONSE BODY")

	s.httpResponse = response
	s.responseBody = string(responseBody)

	if len(s.responseBody) > 0 {
		_ = json.Unmarshal([]byte(s.responseBody), &s.response)
	}

	return nil
}

func PrettifyJSON(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "  ", " ")
	s = strings.TrimSpace(s)

	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, []byte(s), "", "  "); err != nil {
		return s
	}

	return prettyJSON.String()
}

func (s *ServerFeature) ReplaceValues(input string) string {
	for k, v := range s.replacements {
		input = strings.ReplaceAll(input, fmt.Sprintf("${%s}", k), fmt.Sprint(v))
	}
	input = strings.ReplaceAll(input, "${random_id}", fmt.Sprint(rand.Intn(10000000)))
	input = strings.ReplaceAll(input, "${today}", time.Now().Format(time.DateOnly))

	found := false
	for strings.Contains(input, "${") {
		for k, v := range s.store {

			if strings.Contains(input, fmt.Sprintf("${%s}", k)) {
				input = strings.ReplaceAll(input, fmt.Sprintf("${%s}", k), fmt.Sprint(v))
				found = true

			} else if strings.Contains(input, fmt.Sprintf("${%s.", k)) {
				start := strings.Index(input, fmt.Sprintf("${%s.", k))
				if start == -1 {
					break
				}

				end := strings.Index(input[start:], "}")
				if end == -1 {
					break
				}

				key := input[start+len(k)+3 : start+end]
				val, ok := v.(map[string]interface{})[key]
				if !ok {
					break
				}

				input = fmt.Sprintf("%s%s%s", input[:start], val, input[start+end+1:])
				found = true
				break
			}

		}

		if !found {
			break
		}
	}

	return input
}

func (s *ServerFeature) FormatURL(endpoint string) (baseURL *url.URL) {
	appDomain := viper.GetString("appDomain")

	scheme := "http"
	domain := "localhost:8080"

	lifecycle := viper.GetString("lifecycle")

	if lifecycle != "local" {
		scheme = "https"
		if lifecycle == "prod" {
			domain = appDomain
		} else {
			domain = fmt.Sprintf("%s.%s", lifecycle, appDomain)
		}
	}

	return &url.URL{
		Scheme: scheme,
		Host:   domain,
		Path:   "/api/" + endpoint,
	}
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	api := &ServerFeature{client: http.DefaultClient}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		api.reset(sc)
		return ctx, nil
	})

	ctx.Step(`^I send "(GET|POST|DELETE)" request to "([^"]*)"$`, api.SendRequest)
	ctx.Step(`^I send "(PATCH|POST|PUT)" request to "([^"]*)" with data$`, api.SendRequestWithData)
	ctx.Step(`^I send "(GET|POST|PUT|PATCH|DELETE)" request to "([^"]*)" with params$`, api.SendRequestWithParams)

	ctx.Step(`^the response code should be (\d+)$`, api.TheResponseCodeShouldBe)
	ctx.Step(`^the response should not be empty$`, api.TheResponseShouldNotBeEmpty)

	ctx.Step(`^the response should match json$`, api.TheResponseShouldMatchJSON)
	ctx.Step(`^the response should contain$`, api.TheResponseShouldContain)
	ctx.Step(`^the response should contain a "([^"]*)"$`, api.TheResponseShouldContainA)
	ctx.Step(`^the response should contain a "([^"]*)" that contains items$`, api.TheResponseShouldContainAWithItems)
	ctx.Step(`^the response should not contain a "([^"]*)"$`, api.TheResponseShouldNotContainA)
	ctx.Step(`^the response should contain a$`, api.TheResponseShouldContainA)

	ctx.Step(`^the response should contain a "([^"]*)" set to "([^"]*)"$`, api.TheResponseShouldContainSetTo)
	ctx.Step(`^the response should contain a "([^"]*)" temporally equal to "([^"]*)"$`, api.TheResponseShouldContainATimeSetTo)
	ctx.Step(`^the response should contain an item at index (\d+) with "([^"]*)" set to "([^"]*)"$`, api.TheResponseContainsItemAtIndexWithPropertySetTo)
	ctx.Step(`^the response should contain an item with "([^"]*)" set to "([^"]*)"$`, api.TheResponseContainsItemWithPropertySetTo)

	ctx.Step(`^the response should contain a "([^"]*)" that is null$`, api.TheResponseShouldContainAThatIsNull)
	ctx.Step(`^the response should contain a "([^"]*)" that is not null$`, api.TheResponseShouldContainAThatIsNotNull)

	ctx.Step(`^the response should contain a "([^"]*)" that is empty$`, api.TheResponseShouldContainAThatIsEmpty)
	ctx.Step(`^the response should contain a "([^"]*)" that is not empty$`, api.TheResponseShouldContainAThatIsNotEmpty)

	ctx.Step(`^the response should have a length of (\d+)$`, api.TheResponseHaveLength)
	ctx.Step(`^the response should contain a "([^"]*)" with length (\d+)$`, api.TheResponseShouldContainAWithLength)

	ctx.Step(`^I save "([^"]*)" from the response`, api.SaveValueFromResponse)
	ctx.Step(`^I save the item at index (\d+) in "([^"]*)" as "([^"]*)"$`, api.SaveValueFromResponseList)
}
