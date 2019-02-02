// Package asana provides a client for the Asana API
package asana // import "bitbucket.org/mikehouston/asana-go"

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	"github.com/google/go-querystring/query"
	"github.com/imdario/mergo"
	"github.com/pkg/errors"
)

const (
	// BaseURL is the default URL used to access the Asana API
	BaseURL = "https://app.asana.com/api/1.0"
)

// Client is the root client for the Asana API. The nested HTTPClient should provide
// Authorization header injection.
type Client struct {
	BaseURL    *url.URL
	HTTPClient *http.Client

	Debug          bool
	Verbose        []bool
	FastAPI        bool
	DefaultOptions Options
}

// NewClient instantiates a new Asana client with the given HTTP client and
// the default base URL
func NewClient(httpClient *http.Client) *Client {
	u, _ := url.Parse(BaseURL)
	return &Client{
		BaseURL:    u,
		FastAPI:    true,
		HTTPClient: httpClient,
	}
}

// A POST API request
type request struct {
	Data    interface{} `json:"data"`
	Options *Options    `json:"options,omitempty"`
}

type NextPage struct {
	Offset string `json:"offset"`
	Path   string `json:"path"`
	URI    string `json:"uri"`
}

// An API response
type Response struct {
	Data     json.RawMessage `json:"data"`
	NextPage *NextPage       `json:"next_page"`
	Errors   []*Error        `json:"errors"`
}

func (c *Client) getURL(path string) string {
	if path[0] != '/' {
		panic("Invalid API path")
	}
	return c.BaseURL.String() + path
}

func mergeQuery(q url.Values, request interface{}) error {
	queryParams, err := query.Values(request)
	if err != nil {
		return errors.Wrap(err, "Unable to marshal request to query parameters")
	}

	// Merge with defaults
	for key, values := range queryParams {
		q.Del(key)
		for _, value := range values {
			q.Add(key, value)
		}
	}

	return nil
}

func (c *Client) get(path string, data, result interface{}, opts ...*Options) (*NextPage, error) {

	// Encode default options
	if c.Debug {
		log.Printf("Default options: %+v", c.DefaultOptions)
	}
	q, err := query.Values(c.DefaultOptions)
	if err != nil {
		return nil, errors.Wrap(err, "Unable to marshal DefaultOptions to query parameters")
	}

	// Encode data
	if data != nil {
		if c.Debug {
			log.Printf("Data: %+v", data)
		}

		// Validate
		if validator, ok := data.(Validator); ok {
			if err := validator.Validate(); err != nil {
				return nil, err
			}
		}

		if err := mergeQuery(q, data); err != nil {
			return nil, err
		}
	}

	// Encode query options
	for _, options := range opts {
		if c.Debug {
			log.Printf("Options: %+v", options)
		}
		if err := mergeQuery(q, options); err != nil {
			return nil, err
		}
	}
	if len(q) > 0 {
		path = path + "?" + q.Encode()
	}

	// Make request
	if c.Debug {
		log.Printf("GET %s", path)
	}
	request, err := http.NewRequest(http.MethodGet, c.getURL(path), nil)
	if err != nil {
		return nil, errors.Wrap(err, "Request error")
	}
	if c.FastAPI {
		request.Header.Add("Asana-Fast-Api", "true")
	}
	resp, err := c.HTTPClient.Do(request)
	if err != nil {
		return nil, errors.Wrap(err, "GET error")
	}

	// Parse the result
	resultData, err := c.parseResponse(resp, result)
	if err != nil {
		return nil, err
	}

	return resultData.NextPage, nil
}

func (c *Client) post(path string, data, result interface{}, opts ...*Options) error {
	return c.do(http.MethodPost, path, data, result, opts...)
}

func (c *Client) put(path string, data, result interface{}, opts ...*Options) error {
	return c.do(http.MethodPut, path, data, result, opts...)
}

func (c *Client) do(method, path string, data, result interface{}, opts ...*Options) error {
	// Prepare options
	var options *Options
	if opts != nil {
		options = opts[0]
	}
	if options == nil {
		options = &Options{}
	}
	if err := mergo.Merge(options, c.DefaultOptions); err != nil {
		return errors.Wrap(err, "unable to merge options")
	}

	// Validate data
	if validator, ok := data.(Validator); ok {
		if err := validator.Validate(); err != nil {
			return err
		}
	}

	// Build request
	req := &request{
		Data:    data,
		Options: options,
	}

	// Encode request body
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	// Make request
	if c.Debug {
		body, _ := json.MarshalIndent(req, "", "  ")
		log.Printf("%s %s\n%s", method, path, body)
	}
	request, err := http.NewRequest(method, c.getURL(path), bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "Request error")
	}

	request.Header.Add("Content-Type", "application/json")
	if c.FastAPI {
		request.Header.Add("Asana-Fast-Api", "true")
	}
	resp, err := c.HTTPClient.Do(request)
	if err != nil {
		return errors.Wrapf(err, "%s error", method)
	}

	_, err = c.parseResponse(resp, result)
	return err
}

// From mime.multipart package ------
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}

// --------

func (c *Client) postMultipart(path string, result interface{}, field string, r io.ReadCloser, filename string, contentType string) error {
	// Make request
	if c.Debug {
		log.Printf("POST multipart %s\n%s=%s;ContentType=%s", path, field, filename, contentType)
	}
	defer r.Close()

	// Write header
	buffer := &bytes.Buffer{}
	partWriter := multipart.NewWriter(buffer)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
			escapeQuotes(field), escapeQuotes(filename)))
	h.Set("Content-Type", contentType)

	_, err := partWriter.CreatePart(h)
	if err != nil {
		return errors.Wrap(err, "create multipart header")
	}
	headerSize := buffer.Len()

	// Write footer
	if err = partWriter.Close(); err != nil {
		return errors.Wrap(err, "create multipart footer")
	}

	// Create request
	request, err := http.NewRequest(http.MethodPost, c.getURL(path), io.MultiReader(
		bytes.NewReader(buffer.Bytes()[:headerSize]),
		r,
		bytes.NewReader(buffer.Bytes()[headerSize:])))
	if err != nil {
		return errors.Wrap(err, "Request error")
	}

	request.Header.Add("Content-Type", partWriter.FormDataContentType())
	if c.FastAPI {
		request.Header.Add("Asana-Fast-Api", "true")
	}
	resp, err := c.HTTPClient.Do(request)
	if err != nil {
		return errors.Wrapf(err, "POST error")
	}

	_, err = c.parseResponse(resp, result)
	return err
}

func (c *Client) parseResponse(resp *http.Response, result interface{}) (*Response, error) {

	// Get response body
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if c.Debug {
		log.Printf("%s\n%s", resp.Status, body)
	}

	// Decode the response
	value := &Response{}
	if err := json.Unmarshal(body, value); err != nil {
		return nil, err
	}

	// Check for errors
	switch resp.StatusCode {
	case 200: // OK
	case 201: // Object created
	default:
		return nil, value.Error(resp)
	}

	// Decode the data field
	if value.Data == nil {
		return nil, errors.New("Missing data from response")
	}

	return value, c.parseResponseData(value.Data, result)
}

func (c *Client) parseResponseData(data []byte, result interface{}) error {
	if err := json.Unmarshal(data, result); err != nil {
		return err
	}

	return nil
}
