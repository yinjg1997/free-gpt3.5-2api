package FreeGpt35

import (
	"encoding/json"
	"errors"
	"fmt"
	"free-gpt3.5-2api/ProxyPool"
	"free-gpt3.5-2api/RequestClient"
	"free-gpt3.5-2api/common"
	"free-gpt3.5-2api/config"
	"github.com/aurorax-neo/go-logger"
	fhttp "github.com/bogdanfinn/fhttp"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/google/uuid"
	"io"
	"strings"
)

const BaseUrl = "https://chat.openai.com"
const ApiUrl = BaseUrl + "/backend-anon/conversation"
const SessionUrl = BaseUrl + "/backend-anon/sentinel/chat-requirements"

type Gpt35 struct {
	RequestClient RequestClient.RequestClient
	Proxy         *ProxyPool.Proxy
	MaxUseCount   int
	ExpiresIn     int64
	Session       *session
	Ua            string
	Cookies       []*fhttp.Cookie
	Language      string
}

type session struct {
	OaiDeviceId string    `json:"-"`
	Persona     string    `json:"persona"`
	Arkose      arkose    `json:"arkose"`
	Turnstile   turnstile `json:"turnstile"`
	ProofWork   ProofWork `json:"proofofwork"`
	Token       string    `json:"token"`
}

type arkose struct {
	Required bool   `json:"required"`
	Dx       string `json:"dx"`
}

type turnstile struct {
	Required bool `json:"required"`
}

// NewGpt35 创建 Gpt35 实例 0 获取 1 刷新获取
func NewGpt35(newType int) *Gpt35 {
	// 创建 FreeGpt35 实例
	gpt35 := &Gpt35{
		MaxUseCount: -1,
		ExpiresIn:   -1,
		Session:     &session{},
		Ua:          "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/44.0.0.0 Safari/537.36",
	}
	// 获取请求客户端
	err := gpt35.getNewRequestClient(newType)
	if err != nil {
		return nil
	}
	// 获取 cookies
	err = gpt35.getCookies()
	if err != nil {
		return nil
	}
	// 获取新session
	err = gpt35.getNewSession()
	if err != nil {
		return nil
	}
	return gpt35
}

func (G *Gpt35) getNewRequestClient(newType int) error {
	// 获取代理池
	ProxyPoolInstance := ProxyPool.GetProxyPoolInstance()
	// 获取代理
	G.Proxy = ProxyPoolInstance.GetProxy()
	// 判断代理是否可用
	if G.Proxy.CanUseAt > common.GetTimestampSecond(0) && newType == 1 {
		errStr := fmt.Sprint(G.Proxy.Link, ": Proxy restricted, Reuse at ", G.Proxy.CanUseAt)
		return fmt.Errorf(errStr)
	}
	// 请求客户端
	G.RequestClient = RequestClient.NewTlsClient(300, profiles.Safari_15_6_1)
	if G.RequestClient == nil {
		errStr := fmt.Sprint("RequestClient is nil")
		logger.Logger.Debug(errStr)
		return fmt.Errorf(errStr)
	}
	// 设置代理
	err := G.RequestClient.SetProxy(G.Proxy.Link.String())
	if err != nil {
		errStr := fmt.Sprint("SetProxy Error: ", err)
		logger.Logger.Debug(errStr)
	}
	// 成功后更新代理的可用时间
	G.Proxy.CanUseAt = common.GetTimestampSecond(0)
	return nil
}

func (G *Gpt35) getCookies() error {
	var data = strings.NewReader(`{}`)
	req, err := RequestClient.NewRequest("POST", "https://chat.openai.com/cdn-cgi/challenge-platform/h/b/jsd/r/"+common.RandomHexadecimalString(), data)
	if err != nil {
		return err
	}
	resp, err := G.RequestClient.Do(req)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	if resp.StatusCode != 200 {
		return errors.New("failed to get cookies")
	}
	for _, cookie := range resp.Cookies() {
		G.Cookies = append(G.Cookies, cookie)
	}
	return nil
}

func (G *Gpt35) getNewSession() error {
	// 生成新的设备 ID
	G.Session.OaiDeviceId = uuid.New().String()
	// 创建请求
	request, err := G.NewRequest("POST", SessionUrl, nil)
	if err != nil {
		return err
	}
	// 设置请求头
	request.Header.Set("oai-device-id", G.Session.OaiDeviceId)
	// 发送 POST 请求
	response, err := G.RequestClient.Do(request)
	if err != nil {
		return err
	}
	if response.StatusCode != 200 {
		if response.StatusCode == 429 {
			G.Proxy.CanUseAt = common.GetTimestampSecond(600)
			logger.Logger.Debug(fmt.Sprint("getNewSession: Proxy restricted, Reuse at ", G.Proxy.CanUseAt))
		}
		logger.Logger.Debug(fmt.Sprint("getNewSession: StatusCode: ", response.StatusCode))
		return fmt.Errorf("StatusCode: %d", response.StatusCode)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(response.Body)
	if err := json.NewDecoder(response.Body).Decode(&G.Session); err != nil {
		return err
	}
	if G.Session.ProofWork.Required {
		G.Session.ProofWork.Ospt = CalcProofToken(G.Session.ProofWork.Seed, G.Session.ProofWork.Difficulty, request.Header.Get("User-Agent"))
	}
	// 设置 MaxUseCount
	G.MaxUseCount = 1
	// 设置 ExpiresIn
	G.ExpiresIn = common.GetTimestampSecond(config.AuthED)
	return nil
}

func (G *Gpt35) NewRequest(method, url string, body io.Reader) (*fhttp.Request, error) {
	request, err := RequestClient.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("origin", common.GetOrigin(BaseUrl))
	request.Header.Set("referer", common.GetOrigin(BaseUrl))
	request.Header.Set("accept", "*/*")
	request.Header.Set("oai-language", "en-US")
	request.Header.Set("cache-control", "no-cache")
	request.Header.Set("content-type", "application/json")
	request.Header.Set("pragma", "no-cache")
	request.Header.Set("sec-ch-ua-mobile", "?0")
	request.Header.Set("sec-fetch-dest", "empty")
	request.Header.Set("sec-fetch-mode", "cors")
	request.Header.Set("sec-fetch-site", "same-origin")
	request.Header.Set("Sec-Ch-Ua", `"Chromium";v="122", "Not(A:Brand";v="24", "Google Chrome";v="122"`)
	request.Header.Set("accept-language", "zh-CN,zh;q=0.9,zh-Hans;q=0.8,en;q=0.7")
	request.Header.Set("User-Agent", G.Ua)
	for _, cookie := range G.Cookies {
		request.AddCookie(cookie)
	}
	return request, nil
}
