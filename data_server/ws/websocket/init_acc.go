/**
 * Created by GoLand.
 * User: link1st
 * Date: 2019-07-25
 * Time: 16:04
 */

package websocket

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"latest/api/ws/helper"

	"github.com/gorilla/websocket"
)

const (
	defaultAppId = 101 // 默认平台Id
)

var (
	clientManager = NewClientManager()                    // 管理者
	appIds        = []uint32{defaultAppId, 102, 103, 104} // 全部的平台

	serverIp   string
	serverPort string
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var dialer = websocket.Dialer{}

func GetAppIds() []uint32 {

	return appIds
}

// func GetServer() (server *models.Server) {
// 	server = models.NewServer(serverIp, serverPort)

// 	return
// }

// func IsLocal(server *models.Server) (isLocal bool) {
// 	if server.Ip == serverIp && server.Port == serverPort {
// 		isLocal = true
// 	}

// 	return
// }

func InAppIds(appId uint32) (inAppId bool) {

	for _, value := range appIds {
		if value == appId {
			inAppId = true

			return
		}
	}

	return
}

func GetDefaultAppId() (appId uint32) {
	appId = defaultAppId

	return
}

// 启动程序
func StartWebSocket(w http.ResponseWriter, r *http.Request) {

	serverIp = helper.GetServerIp()

	wsPage(w, r)

	// 添加处理程序
	go clientManager.start()
	// fmt.Println("WebSocket 启动程序成功", serverIp, serverPort)

	// http.ListenAndServe(":"+webSocketPort, nil)
}

func wsPage(w http.ResponseWriter, req *http.Request) {

	// 升级协议
	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		log.Println(err)
		return
	}

	fmt.Println("webSocket 建立连接:", conn.RemoteAddr().String())

	currentTime := uint64(time.Now().Unix())
	client := NewClient(conn.RemoteAddr().String(), conn, currentTime)
	go client.Read()
	go client.Write()

	// 用户连接事件
	clientManager.Register <- client
}
