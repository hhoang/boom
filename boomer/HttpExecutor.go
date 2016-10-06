package boomer

import (
	"net/http"
	"crypto/tls"
	"bytes"
	"fmt"
	"io/ioutil"
	"time"
	"io"
)

var _ BoomExecutor = (*HttpExecutor)(nil)

type HttpExecutor struct{
	request *http.Request
	body []byte
}

func NewHttpExecutor(request *http.Request) *HttpExecutor{
	return &HttpExecutor{request: request, body: copyReqBody(request)}
}

func (h *HttpExecutor) GetHost() string{
	return h.request.URL.Host
}

func (h *HttpExecutor) GetEndpoint() string {
	return h.request.URL.Path
}

func (h *HttpExecutor) GetRequestType() string {
	return fmt.Sprintf("http%v", h.request.Method)
}

func (h *HttpExecutor) Execute(results chan *Result){
	client := getDefaultHttpClient()
	s := time.Now()
	var size int64
	var code int

	request := h.cloneRequest()
	resp, err := client.Do(request)

	if err == nil {
		size = resp.ContentLength
		code = resp.StatusCode
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
	}
	results <- &Result{
		StatusCode:    code,
		Duration:      time.Now().Sub(s),
		Err:           err,
		ContentLength: size,
	}
}

func getDefaultHttpClient() *http.Client{
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		DisableCompression: false,
		DisableKeepAlives:  false,
		// TODO(jbd): Add dial timeout.
		TLSHandshakeTimeout: 0,
	}
	return &http.Client{Transport: tr}
}

// cloneRequest returns a clone of the provided *http.Request.
// The clone is a shallow copy of the struct and its Header map.
func (h *HttpExecutor) cloneRequest() *http.Request {
	// shallow copy of the struct
	r2 := new(http.Request)
	*r2 = *h.request
	// deep copy of the Header
	r2.Header = make(http.Header, len(h.request.Header))
	for k, s := range h.request.Header {
		r2.Header[k] = append([]string(nil), s...)
	}

	if h.body != nil{
		r2.Body = ioutil.NopCloser(bytes.NewReader(h.body))
	}
	return r2
}

func copyReqBody(req *http.Request) (body []byte){
	if req.Body == nil {
		return nil
	}

	body, err := ioutil.ReadAll(req.Body)

	// restore the state of the request body
	req.Body = ioutil.NopCloser(bytes.NewReader(body))
	if err != nil {
		fmt.Println("Error cloning request : ", err)
	}
	return
}