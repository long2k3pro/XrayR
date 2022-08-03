package proxypanel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/realldz/XrayR/api"
)

// APIClient create a api client to the panel.
type APIClient struct {
	client           *resty.Client
	APIHost          string
	NodeID           int
	Key              string
	NodeType         string
	EnableVless      bool
	EnableXTLS       bool
	SpeedLimit       float64
	DeviceLimit      int
	LocalRuleList    []api.DetectRule
	LastReportOnline map[int]int
	access           sync.Mutex
}

// New creat a api instance
func New(apiConfig *api.Config) *APIClient {

	client := resty.New()
	client.SetRetryCount(3)
	if apiConfig.Timeout > 0 {
		client.SetTimeout(time.Duration(apiConfig.Timeout) * time.Second)
	} else {
		client.SetTimeout(5 * time.Second)
	}
	client.OnError(func(req *resty.Request, err error) {
		if v, ok := err.(*resty.ResponseError); ok {
			// v.Response contains the last response from the server
			// v.Err contains the original error
			log.Print(v.Err)
		}
	})
	client.SetBaseURL(apiConfig.APIHost)
	// Read local rule list
	localRuleList := readLocalRuleList(apiConfig.RuleListPath)
	apiClient := &APIClient{
		client:           client,
		NodeID:           apiConfig.NodeID,
		Key:              apiConfig.Key,
		APIHost:          apiConfig.APIHost,
		NodeType:         apiConfig.NodeType,
		EnableVless:      apiConfig.EnableVless,
		EnableXTLS:       apiConfig.EnableXTLS,
		SpeedLimit:       apiConfig.SpeedLimit,
		DeviceLimit:      apiConfig.DeviceLimit,
		LocalRuleList:    localRuleList,
		LastReportOnline: make(map[int]int),
	}
	return apiClient
}

// readLocalRuleList reads the local rule list file
func readLocalRuleList(path string) (LocalRuleList []api.DetectRule) {

	LocalRuleList = make([]api.DetectRule, 0)
	if path != "" {
		// open the file
		file, err := os.Open(path)

		//handle errors while opening
		if err != nil {
			log.Printf("Error when opening file: %s", err)
			return LocalRuleList
		}

		fileScanner := bufio.NewScanner(file)

		// read line by line
		for fileScanner.Scan() {
			LocalRuleList = append(LocalRuleList, api.DetectRule{
				ID:      -1,
				Pattern: regexp.MustCompile(fileScanner.Text()),
			})
		}
		// handle first encountered error while reading
		if err := fileScanner.Err(); err != nil {
			log.Fatalf("Error while reading file: %s", err)
			return make([]api.DetectRule, 0)
		}

		file.Close()
	}

	return LocalRuleList
}

// Describe return a description of the client
func (c *APIClient) Describe() api.ClientInfo {
	return api.ClientInfo{APIHost: c.APIHost, NodeID: c.NodeID, Key: c.Key, NodeType: c.NodeType}
}

// Debug set the client debug for client
func (c *APIClient) Debug() {
	c.client.SetDebug(true)
}

func (c *APIClient) assembleURL(path string) string {
	return c.APIHost + path
}

func (c *APIClient) createCommonRequest() *resty.Request {
	request := c.client.R().EnableTrace()
	request.EnableTrace()
	request.SetHeader("key", c.Key)
	request.SetHeader("timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	return request
}

func (c *APIClient) parseResponse(res *resty.Response, path string, err error) (*Response, error) {
	if err != nil {
		return nil, fmt.Errorf("request %s failed: %s", c.assembleURL(path), err)
	}

	if res.StatusCode() > 400 {
		body := res.Body()
		return nil, fmt.Errorf("request %s failed: %s, %s", c.assembleURL(path), string(body), err)
	}
	response := res.Result().(*Response)

	if response.Status != "success" {
		res, _ := json.Marshal(&response)
		return nil, fmt.Errorf("Ret %s invalid", string(res))
	}
	return response, nil
}

// GetNodeInfo will pull NodeInfo Config from sspanel
func (c *APIClient) GetNodeInfo() (nodeInfo *api.NodeInfo, err error) {
	var path string
	switch c.NodeType {
	case "V2ray":
		path = fmt.Sprintf("/api/v2ray/v1/node/%d", c.NodeID)
	case "Trojan":
		path = fmt.Sprintf("/api/trojan/v1/node/%d", c.NodeID)
	case "Shadowsocks":
		path = fmt.Sprintf("/api/ss/v1/node/%d", c.NodeID)
	default:
		return nil, fmt.Errorf("Unsupported Node type: %s", c.NodeType)
	}

	res, err := c.createCommonRequest().
		SetResult(&Response{}).
		ForceContentType("application/json").
		Get(path)

	response, err := c.parseResponse(res, path, err)
	if err != nil {
		return nil, err
	}

	switch c.NodeType {
	case "V2ray":
		nodeInfo, err = c.ParseV2rayNodeResponse(&response.Data)
	case "Trojan":
		nodeInfo, err = c.ParseTrojanNodeResponse(&response.Data)
	case "Shadowsocks":
		nodeInfo, err = c.ParseSSNodeResponse(&response.Data)
	default:
		return nil, fmt.Errorf("Unsupported Node type: %s", c.NodeType)
	}

	if err != nil {
		res, _ := json.Marshal(response.Data)
		return nil, fmt.Errorf("Parse node info failed: %s, \nError: %s", string(res), err)
	}

	return nodeInfo, nil
}

// GetUserList will pull user form sspanel
func (c *APIClient) GetUserList() (UserList *[]api.UserInfo, err error) {
	var path string
	switch c.NodeType {
	case "V2ray":
		path = fmt.Sprintf("/api/v2ray/v1/userList/%d", c.NodeID)
	case "Trojan":
		path = fmt.Sprintf("/api/trojan/v1/userList/%d", c.NodeID)
	case "Shadowsocks":
		path = fmt.Sprintf("/api/ss/v1/userList/%d", c.NodeID)
	default:
		return nil, fmt.Errorf("Unsupported Node type: %s", c.NodeType)
	}

	res, err := c.createCommonRequest().
		SetResult(&Response{}).
		ForceContentType("application/json").
		Get(path)

	response, err := c.parseResponse(res, path, err)
	if err != nil {
		return nil, err
	}
	userList := new([]api.UserInfo)
	switch c.NodeType {
	case "V2ray":
		userList, err = c.ParseV2rayUserListResponse(&response.Data)
	case "Trojan":
		userList, err = c.ParseTrojanUserListResponse(&response.Data)
	case "Shadowsocks":
		userList, err = c.ParseSSUserListResponse(&response.Data)
	default:
		return nil, fmt.Errorf("Unsupported Node type: %s", c.NodeType)
	}
	if err != nil {
		res, _ := json.Marshal(response.Data)
		return nil, fmt.Errorf("Parse user list failed: %s", string(res))
	}
	return userList, nil
}

// ReportNodeStatus reports the node status to the sspanel
func (c *APIClient) ReportNodeStatus(nodeStatus *api.NodeStatus) (err error) {
	var path string
	switch c.NodeType {
	case "V2ray":
		path = fmt.Sprintf("/api/v2ray/v1/nodeStatus/%d", c.NodeID)
	case "Trojan":
		path = fmt.Sprintf("/api/trojan/v1/nodeStatus/%d", c.NodeID)
	case "Shadowsocks":
		path = fmt.Sprintf("/api/ss/v1/nodeStatus/%d", c.NodeID)
	default:
		return fmt.Errorf("Unsupported Node type: %s", c.NodeType)
	}

	systemload := NodeStatus{
		Uptime: nodeStatus.Uptime,
		CPU:    fmt.Sprintf("%d%%", int(nodeStatus.CPU)),
		Mem:    fmt.Sprintf("%d%%", int(nodeStatus.Mem)),
		Disk:   fmt.Sprintf("%d%%", int(nodeStatus.Disk)),
	}

	res, err := c.createCommonRequest().
		SetBody(systemload).
		SetResult(&Response{}).
		ForceContentType("application/json").
		Post(path)

	_, err = c.parseResponse(res, path, err)
	if err != nil {
		return err
	}

	return nil
}

//ReportNodeOnlineUsers reports online user ip
func (c *APIClient) ReportNodeOnlineUsers(onlineUserList *[]api.OnlineUser) error {
	c.access.Lock()
	defer c.access.Unlock()
	var path string
	switch c.NodeType {
	case "V2ray":
		path = fmt.Sprintf("/api/v2ray/v1/nodeOnline/%d", c.NodeID)
	case "Trojan":
		path = fmt.Sprintf("/api/trojan/v1/nodeOnline/%d", c.NodeID)
	case "Shadowsocks":
		path = fmt.Sprintf("/api/ss/v1/nodeOnline/%d", c.NodeID)
	default:
		return fmt.Errorf("Unsupported Node type: %s", c.NodeType)
	}
	reportOnline := make(map[int]int)
	data := make([]NodeOnline, len(*onlineUserList))
	for i, user := range *onlineUserList {
		data[i] = NodeOnline{UID: user.UID, IP: user.IP}
		if _, ok := reportOnline[user.UID]; ok {
			reportOnline[user.UID]++
		} else {
			reportOnline[user.UID] = 1
		}
	}
	c.LastReportOnline = reportOnline
	res, err := c.createCommonRequest().
		SetBody(data).
		SetResult(&Response{}).
		ForceContentType("application/json").
		Post(path)

	_, err = c.parseResponse(res, path, err)
	if err != nil {
		return err
	}

	return nil
}

// ReportUserTraffic reports the user traffic
func (c *APIClient) ReportUserTraffic(userTraffic *[]api.UserTraffic) error {
	var path string
	switch c.NodeType {
	case "V2ray":
		path = fmt.Sprintf("/api/v2ray/v1/userTraffic/%d", c.NodeID)
	case "Trojan":
		path = fmt.Sprintf("/api/trojan/v1/userTraffic/%d", c.NodeID)
	case "Shadowsocks":
		path = fmt.Sprintf("/api/ss/v1/userTraffic/%d", c.NodeID)
	default:
		return fmt.Errorf("Unsupported Node type: %s", c.NodeType)
	}

	data := make([]UserTraffic, len(*userTraffic))
	for i, traffic := range *userTraffic {
		data[i] = UserTraffic{
			UID:      traffic.UID,
			Upload:   traffic.Upload,
			Download: traffic.Download}
	}
	res, err := c.createCommonRequest().
		SetBody(data).
		SetResult(&Response{}).
		ForceContentType("application/json").
		Post(path)

	_, err = c.parseResponse(res, path, err)
	if err != nil {
		return err
	}

	return nil
}

// GetNodeRule will pull the audit rule form sspanel
func (c *APIClient) GetNodeRule() (*[]api.DetectRule, *[]string, error) {
	var path string
	switch c.NodeType {
	case "V2ray":
		path = fmt.Sprintf("/api/v2ray/v1/nodeRule/%d", c.NodeID)
	case "Trojan":
		path = fmt.Sprintf("/api/trojan/v1/nodeRule/%d", c.NodeID)
	case "Shadowsocks":
		path = fmt.Sprintf("/api/ss/v1/nodeRule/%d", c.NodeID)
	default:
		return nil, nil, fmt.Errorf("Unsupported Node type: %s", c.NodeType)
	}

	res, err := c.createCommonRequest().
		SetResult(&Response{}).
		ForceContentType("application/json").
		Get(path)

	response, err := c.parseResponse(res, path, err)
	if err != nil {
		return nil, nil, err
	}

	ruleListResponse := new(NodeRule)

	if err := json.Unmarshal(response.Data, ruleListResponse); err != nil {
		return nil, nil, fmt.Errorf("Unmarshal %s failed: %s", reflect.TypeOf(ruleListResponse), err)
	}
	ruleList := c.LocalRuleList
	// Only support reject rule type
	if ruleListResponse.Mode != "reject" {
		return &ruleList, nil, nil
	} else {
		for _, r := range ruleListResponse.Rules {
			if r.Type == "reg" {
				ruleList = append(ruleList, api.DetectRule{
					ID:      r.ID,
					Pattern: regexp.MustCompile(r.Pattern),
				})
			}

		}
	}

	return &ruleList, nil, nil
}

// ReportIllegal reports the user illegal behaviors
func (c *APIClient) ReportIllegal(detectResultList *[]api.DetectResult) error {
	var path string
	switch c.NodeType {
	case "V2ray":
		path = fmt.Sprintf("/api/v2ray/v1/trigger/%d", c.NodeID)
	case "Trojan":
		path = fmt.Sprintf("/api/trojan/v1/trigger/%d", c.NodeID)
	case "Shadowsocks":
		path = fmt.Sprintf("/api/ss/v1/trigger/%d", c.NodeID)
	default:
		return fmt.Errorf("Unsupported Node type: %s", c.NodeType)
	}

	for _, r := range *detectResultList {
		res, err := c.createCommonRequest().
			SetBody(IllegalReport{
				RuleID: r.RuleID,
				UID:    r.UID,
				Reason: "XrayR cannot save reason",
			}).
			SetResult(&Response{}).
			ForceContentType("application/json").
			Post(path)

		_, err = c.parseResponse(res, path, err)
		if err != nil {
			return err
		}
	}

	return nil
}

// ParseV2rayNodeResponse parse the response for the given nodeinfor format
func (c *APIClient) ParseV2rayNodeResponse(nodeInfoResponse *json.RawMessage) (*api.NodeInfo, error) {
	var TLStype string
	var speedlimit uint64 = 0
	var devicelimit int = 0
	var header json.RawMessage
	if c.EnableXTLS {
		TLStype = "xtls"
	} else {
		TLStype = "tls"
	}

	v2rayNodeInfo := new(V2rayNodeInfo)
	if err := json.Unmarshal(*nodeInfoResponse, v2rayNodeInfo); err != nil {
		return nil, fmt.Errorf("Unmarshal %s failed: %s", reflect.TypeOf(*nodeInfoResponse), err)
	}

	if c.SpeedLimit > 0 {
		speedlimit = uint64((c.SpeedLimit * 1000000) / 8)
	} else {
		speedlimit = uint64(v2rayNodeInfo.SpeedLimit)
	}

	if c.DeviceLimit > 0 {
		devicelimit = c.DeviceLimit
	} else {
		devicelimit = v2rayNodeInfo.ClientLimit
	}

	if v2rayNodeInfo.V2Type == "http" {
		in := `{"type":"http","request": {"path":"` + v2rayNodeInfo.V2Path + `"}}`
		header = json.RawMessage(in)
	}
	// Create GeneralNodeInfo
	nodeinfo := &api.NodeInfo{
		NodeType:          c.NodeType,
		NodeID:            c.NodeID,
		Port:              v2rayNodeInfo.V2Port,
		SpeedLimit:        speedlimit,
		DeviceLimit:       devicelimit,
		AlterID:           v2rayNodeInfo.V2AlterID,
		TransportProtocol: v2rayNodeInfo.V2Net,
		FakeType:          v2rayNodeInfo.V2Type,
		ServiceName:       v2rayNodeInfo.V2Path,
		Header:            header,
		EnableTLS:         v2rayNodeInfo.V2TLS,
		TLSType:           TLStype,
		Path:              v2rayNodeInfo.V2Path,
		Host:              v2rayNodeInfo.V2Host,
		EnableVless:       c.EnableVless,
	}

	return nodeinfo, nil
}

// ParseSSNodeResponse parse the response for the given nodeinfor format
func (c *APIClient) ParseSSNodeResponse(nodeInfoResponse *json.RawMessage) (*api.NodeInfo, error) {
	var speedlimit uint64 = 0
	var devicelimit int = 0
	shadowsocksNodeInfo := new(ShadowsocksNodeInfo)
	if err := json.Unmarshal(*nodeInfoResponse, shadowsocksNodeInfo); err != nil {
		return nil, fmt.Errorf("Unmarshal %s failed: %s", reflect.TypeOf(*nodeInfoResponse), err)
	}
	if c.SpeedLimit > 0 {
		speedlimit = uint64((c.SpeedLimit * 1000000) / 8)
	} else {
		speedlimit = uint64(shadowsocksNodeInfo.SpeedLimit)
	}

	if c.DeviceLimit > 0 {
		devicelimit = c.DeviceLimit
	} else {
		devicelimit = shadowsocksNodeInfo.ClientLimit
	}

	// Create GeneralNodeInfo
	nodeinfo := &api.NodeInfo{
		NodeType:          c.NodeType,
		NodeID:            c.NodeID,
		Port:              shadowsocksNodeInfo.Port,
		SpeedLimit:        speedlimit,
		DeviceLimit:       devicelimit,
		TransportProtocol: "tcp",
		CypherMethod:      shadowsocksNodeInfo.Method,
	}

	return nodeinfo, nil
}

// ParseTrojanNodeResponse parse the response for the given nodeinfor format
func (c *APIClient) ParseTrojanNodeResponse(nodeInfoResponse *json.RawMessage) (*api.NodeInfo, error) {

	var TLSType string
	var speedlimit uint64 = 0
	var devicelimit int = 0
	if c.EnableXTLS {
		TLSType = "xtls"
	} else {
		TLSType = "tls"
	}

	trojanNodeInfo := new(TrojanNodeInfo)
	if err := json.Unmarshal(*nodeInfoResponse, trojanNodeInfo); err != nil {
		return nil, fmt.Errorf("Unmarshal %s failed: %s", reflect.TypeOf(*nodeInfoResponse), err)
	}
	if c.SpeedLimit > 0 {
		speedlimit = uint64((c.SpeedLimit * 1000000) / 8)
	} else {
		speedlimit = uint64(trojanNodeInfo.SpeedLimit)
	}

	if c.DeviceLimit > 0 {
		devicelimit = c.DeviceLimit
	} else {
		devicelimit = trojanNodeInfo.ClientLimit
	}
	// Create GeneralNodeInfo
	nodeinfo := &api.NodeInfo{
		NodeType:          c.NodeType,
		NodeID:            c.NodeID,
		Port:              trojanNodeInfo.TrojanPort,
		DeviceLimit:       devicelimit,
		SpeedLimit:        speedlimit,
		TransportProtocol: "tcp",
		EnableTLS:         true,
		TLSType:           TLSType,
	}

	return nodeinfo, nil
}

// ParseV2rayUserListResponse parse the response for the given userinfo format
func (c *APIClient) ParseV2rayUserListResponse(userInfoResponse *json.RawMessage) (*[]api.UserInfo, error) {
	var speedlimit uint64 = 0
	var devicelimit, localDeviceLimit int = 0, 0

	vmessUserList := new([]*VMessUser)
	if err := json.Unmarshal(*userInfoResponse, vmessUserList); err != nil {
		return nil, fmt.Errorf("Unmarshal %s failed: %s", reflect.TypeOf(*userInfoResponse), err)
	}

	userList := []api.UserInfo{}
	for _, user := range *vmessUserList {
		if c.SpeedLimit > 0 {
			speedlimit = uint64((c.SpeedLimit * 1000000) / 8)
		} else {
			speedlimit = uint64(user.SpeedLimit)
		}
		if c.DeviceLimit > 0 {
			devicelimit = c.DeviceLimit
		} else {
			devicelimit = user.DeviceLimit
		}
		if devicelimit > 0 && user.OnlineCount > 0 {
			lastOnline := 0
			if v, ok := c.LastReportOnline[user.UID]; ok {
				lastOnline = v
			}
			if localDeviceLimit = devicelimit - user.OnlineCount + lastOnline; localDeviceLimit > 0 {
				devicelimit = localDeviceLimit
			} else if lastOnline > 0 {
				devicelimit = lastOnline
			} else {
				continue
			}
		} else if _, ok := c.LastReportOnline[user.UID]; user.OnlineCount == 0 && ok {
			delete(c.LastReportOnline, user.UID)
		}
		userList = append(userList, api.UserInfo{
			UID:         user.UID,
			Email:       "",
			UUID:        user.VmessUID,
			DeviceLimit: devicelimit,
			SpeedLimit:  speedlimit,
		})
	}

	return &userList, nil
}

// ParseTrojanUserListResponse parse the response for the given userinfo format
func (c *APIClient) ParseTrojanUserListResponse(userInfoResponse *json.RawMessage) (*[]api.UserInfo, error) {
	var speedlimit uint64 = 0
	var devicelimit, localDeviceLimit int = 0, 0

	trojanUserList := new([]*TrojanUser)
	if err := json.Unmarshal(*userInfoResponse, trojanUserList); err != nil {
		return nil, fmt.Errorf("Unmarshal %s failed: %s", reflect.TypeOf(*userInfoResponse), err)
	}

	userList := []api.UserInfo{}
	for _, user := range *trojanUserList {
		if c.SpeedLimit > 0 {
			speedlimit = uint64((c.SpeedLimit * 1000000) / 8)
		} else {
			speedlimit = uint64(user.SpeedLimit)
		}
		if c.DeviceLimit > 0 {
			devicelimit = c.DeviceLimit
		} else {
			devicelimit = user.DeviceLimit
		}
		if devicelimit > 0 && user.OnlineCount > 0 {
			lastOnline := 0
			if v, ok := c.LastReportOnline[user.UID]; ok {
				lastOnline = v
			}
			if localDeviceLimit = devicelimit - user.OnlineCount + lastOnline; localDeviceLimit > 0 {
				devicelimit = localDeviceLimit
			} else if lastOnline > 0 {
				devicelimit = lastOnline
			} else {
				continue
			}
		} else if _, ok := c.LastReportOnline[user.UID]; user.OnlineCount == 0 && ok {
			delete(c.LastReportOnline, user.UID)
		}
		userList = append(userList, api.UserInfo{
			UID:         user.UID,
			Email:       "",
			UUID:        user.Password,
			DeviceLimit: devicelimit,
			SpeedLimit:  speedlimit,
		})
	}

	return &userList, nil
}

// ParseSSUserListResponse parse the response for the given userinfo format
func (c *APIClient) ParseSSUserListResponse(userInfoResponse *json.RawMessage) (*[]api.UserInfo, error) {
	var speedlimit uint64 = 0

	ssUserList := new([]*SSUser)
	if err := json.Unmarshal(*userInfoResponse, ssUserList); err != nil {
		return nil, fmt.Errorf("Unmarshal %s failed: %s", reflect.TypeOf(*userInfoResponse), err)
	}

	userList := make([]api.UserInfo, len(*ssUserList))
	for i, user := range *ssUserList {
		if c.SpeedLimit > 0 {
			speedlimit = uint64((c.SpeedLimit * 1000000) / 8)
		} else {
			speedlimit = uint64(user.SpeedLimit)
		}
		userList[i] = api.UserInfo{
			UID:         user.UID,
			Email:       "",
			Passwd:      user.Password,
			DeviceLimit: c.DeviceLimit,
			SpeedLimit:  speedlimit,
		}
	}

	return &userList, nil
}
