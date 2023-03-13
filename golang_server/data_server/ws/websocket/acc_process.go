/**
 * Created by GoLand.
 * User: link1st
 * Date: 2019-07-27
 * Time: 14:38
 */

package websocket

import (
	"encoding/json"
	"fmt"
	"sync"

	"latest/api/ws/common"

	"latest/api/ws/models"
)

type DisposeFunc func(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, ocmd string, data interface{}, voice bool)

var (
	handlers        = make(map[string]DisposeFunc)
	handlersRWMutex sync.RWMutex
)

// 注册
func Register(key string, value DisposeFunc) {
	handlersRWMutex.Lock()
	defer handlersRWMutex.Unlock()
	handlers[key] = value

	return
}

func getHandlers(key string) (value DisposeFunc, ok bool) {
	handlersRWMutex.RLock()
	defer handlersRWMutex.RUnlock()

	value, ok = handlers[key]

	return
}

// 处理数据
// func ProcessData(message []byte) {

// 	fmt.Println("处理数据", string(message))

// 	defer func() {
// 		if r := recover(); r != nil {
// 			fmt.Println("处理数据 stop", r)
// 		}
// 	}()
// 	req := models.Request{}
// 	fmt.Println("req", req)
// 	request := &models.Request{}

// 	err := json.Unmarshal(message, request)
// 	if err != nil {
// 		fmt.Println("处理数据 error json Unmarshal", err)
// 		// client.SendMsg([]byte("数据不合法"))

// 		return
// 	}

// 	requestData, err := json.Marshal(request.Data)
// 	if err != nil {
// 		fmt.Println("处理数据 error json Marshal", err)
// 		// client.SendMsg([]byte("处理数据失败"))

// 		return
// 	}

// 	seq := request.Seq
// 	cmd := request.Cmd
// 	ref := request.Ref

// 	var (
// 		code  uint32
// 		msg   string
// 		data  interface{}
// 		voice bool
// 	)

// 	// request
// 	fmt.Println("acc_request", cmd)

// 	// 采用 map 注册的方式
// 	if value, ok := getHandlers(cmd); ok {
// 		code, msg, cmd, data, voice = value(nil, seq, cmd, requestData)
// 	} else {
// 		code = common.RoutingNotExist
// 		fmt.Println("Routing Not Exist", "cmd", cmd)
// 	}
// 	// fmt.Println("msg, data:", msg, data, voice)
// 	fmt.Println("voice:", voice)
// 	msg = common.GetErrorMessage(code, msg)

// 	responseHead := models.NewResponseHead(seq, cmd, ref, code, msg, data)

// 	_, err = json.Marshal(responseHead)
// 	if err != nil {
// 		fmt.Println("处理数据 json Marshal", err)

// 		return
// 	}

// 	// client.SendMsg(headByte)
// 	// fmt.Println("headByte", headByte)
// 	// fmt.Println("acc_response send", "cmd", cmd, "code", code, "data", data)

// 	return
// }

// -----------------------------------------------------------
// When running go, this is the function that is being called
// -----------------------------------------------------------
func ProcessData(client *Client, message []byte) {

	// fmt.Println("Procesamiento de datos", client.Addr, string(message))

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Procesamiento de datos stop ->", r)
		}
	}()

	request := &models.Request{}
	err := json.Unmarshal(message, request)
	if err != nil {
		fmt.Println("Error en Procesamiento de datos json Unmarshal(message, request)", err)
		client.SendMsg([]byte("数据不合法"))

		return
	}
	requestData, err := json.Marshal(request.Data)
	if err != nil {
		fmt.Println("Error en Procesamiento de datos json Marshal(request.Data)", err)
		client.SendMsg([]byte("No se pudieron procesar los datos"))

		return
	}

	seq := request.Seq
	cmd := request.Cmd
	// ref := request.Ref
	fetchId := request.FetchId

	var (
		code  uint32
		msg   string
		data  interface{}
		voice bool
	)

	// request
	// fmt.Println("acc_request", cmd, client.Addr, ref)

	// 采用 map 注册的方式
	if value, ok := getHandlers(cmd); ok {
		code, msg, cmd, data, voice = value(client, seq, cmd, requestData)
	} else {
		code = common.RoutingNotExist
		fmt.Println("El enrutamiento de datos de procesamiento no existe", client.Addr, "cmd", cmd)
	}

	msg = common.GetErrorMessage(code, msg)
	responseHead := models.NewResponseHead(seq, cmd, fetchId, code, msg, data)

	// fmt.Println("Check header before sent", "request.Cmd: ", request.Cmd, "\n ", "original message content: ", string(message), "\n ", "response header: ", responseHead)
	headByte, err := json.Marshal(responseHead)
	if err != nil {
		fmt.Println("处理数据 json Marshal", err)

		return
	}

	client.SendMsg(headByte)
	if voice == true {
		msg = common.GetErrorMessage(code, msg)
		responseHead := models.NewResponseHead(seq, cmd, fetchId, code, msg, data)

		// fmt.Println("Check header before sent", "request.Cmd: ", request.Cmd, "\n ", "original message content: ", string(message), "\n ", "response header: ", responseHead)
		headByte, err := json.Marshal(responseHead)
		if err != nil {
			fmt.Println("处理数据 error json Marshal", err)

			return
		}
		client.SendMsg(headByte)
	}

	// fmt.Println("acc_response send", client.Addr, client.AppId, client.UserId, "cmd", cmd, "code", code, "data", data)

	return
}
