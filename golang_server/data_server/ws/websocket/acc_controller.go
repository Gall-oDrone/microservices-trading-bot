/**
 * Created by GoLand.
 * User: link1st
 * Date: 2019-07-27
 * Time: 13:12
 */

package websocket

import (
	"encoding/json"
	"fmt"
	"time"

	"latest/api/database"
	"latest/api/ws/common"
	"latest/api/ws/controllers"
	"latest/api/ws/models"
	"latest/api/ws/repository/crud"
	"latest/api/ws/utils/console"
)

// ping
func PingController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	code = common.OK
	voice = false
	cmd = icmd
	fmt.Println("webSocket_request ping接口", seq, message)
	data = "pong"

	return
}

func AuthController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	console.PrettyController("AuthController", "WS Request", "blue")
	code = common.OK
	voice = false
	cmd = "auth-good"
	currentTime := uint64(time.Now().Unix())

	req := &models.Login{}
	if err := json.Unmarshal(message, req); err != nil {
		code = common.ParameterIllegal
		cmd = "error_on_controller"
		data = "error"
		fmt.Println("用户登录 解析数据失败", seq, err)

		return
	}
	req.ServiceToken = req.AccessToken
	db, cdb, err := database.Callback()
	if err != nil {
		console.FancyHandleError(err)
		code = common.ServerError
		cmd = "error_on_controller"
		data = "error"
		return
	}
	defer cdb.Close()
	user, err := controllers.GetUser("id", req.UserId, db)
	if err != nil {
		code = common.UnauthorizedUserId
		cmd = "error_on_controller"
		data = "error"
		return
	}
	if !controllers.UserValidation(req, user) {
		code = common.UnauthorizedUserId
		data = "error !controllers.UserValidation"
		fmt.Println("用户登录 非法的用户", seq, req.UserId)
		return
	}

	if client.IsLogin() {
		fmt.Println("用户登录 用户已经登录", client.AppId, client.UserId, seq)
		data = "error"
		code = common.OperationFailure

		return
	}
	console.Pretty(user, "")
	preData := &models.D{}
	preData.SetD(req.ServiceToken)
	if user.DisplayName == "" && user.Bio == "" {
		user.FillUserDataForTesting(user.Username, user.UserId)
		preData.AddUserData(user)
		data = preData
	} else {
		user.LastOnline = time.Now()
		preData.AddUserData(user)
		data = preData
	}
	client.Login(req.AppId, req.UserId, currentTime)

	// 存储数据
	// userOnline := models.UserLogin(serverIp, serverPort, req.AppId, req.UserId, client.Addr, currentTime)
	// err := cache.SetUserOnlineInfo(client.GetKey(), userOnline)
	// if err != nil {
	// 	code = common.ServerError
	// 	fmt.Println("用户登录 SetUserOnlineInfo", seq, err)

	// 	return
	// }

	// 用户登录
	login := &login{
		AppId:  req.AppId,
		UserId: req.UserId,
		Client: client,
	}
	clientManager.Login <- login

	fmt.Println("用户登录 成功", seq, client.Addr, req.UserId)
	return
}

//"search" | "getMyScheduledRoomsAboutToStart" | "joinRoomAndGetInfo" | "getInviteList" | "getFollowList" | "getBlockedFromRoomUsers" | "getMyFollowing" | "getTopPublicRooms" | "getUserProfile" | "getScheduledRooms" | "getRoomUsers"

func RoomController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	voice = false
	code = common.OK
	cmd = icmd + ":reply"
	fmt.Println("Room controller", client.Addr, seq, message)
	fmt.Println("Room controller case room:get_scheduled ")
	request := &models.ScheduledRooms{}
	preData := request.RoomPrepData()
	data = map[string][]models.ScheduledRooms{ // Map literal
		"scheduledRooms": preData,
	}
	// fmt.Println("Check room data", data, preData)

	return
}

func SearchController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("SearchController")
	return
}
func GetMyScheduledRoomsAboutToStartController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("GetMyScheduledRoomsAboutToStartController", seq, message)
	code = common.OK
	voice = false
	cmd = icmd + ":reply"
	request := &models.ScheduledRooms{}
	preData := request.RoomPrepData()
	data = map[string][]models.ScheduledRooms{ // Map literal
		"scheduledRooms": preData,
	}
	// fmt.Println("Check room data", data, preData)

	return
}
func JoinRoomAndGetInfoController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("JoinRoomAndGetInfoController")
	code = common.OK
	voice = true
	cmd = "fetch_done"
	requestedRoomId := &models.PrefetchRoomId{}
	if err := json.Unmarshal(message, requestedRoomId); err != nil {
		code = common.ParameterIllegal
		cmd = "error_on_controller"
		data = "error"
		fmt.Println("用户登录 解析数据失败", seq, err)

		return
	}
	data = models.JoinedRoomPrepData(requestedRoomId.RoomId)
	// fmt.Println("Check JoinRoomAndGetInfoController data: ", data, "\n Check RoomID", requestedRoomId.RoomId)

	return
}
func GetInviteListController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("GetInviteListController")
	return
}
func GetFollowListController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("GetFollowListController")
	fmt.Println("Message raw", string(message))
	code = common.OK
	voice = false
	cmd = "fetch_done"
	request := map[string]interface{}{"cursor": 0, "isFollowing": "", "username": ""}
	if err := json.Unmarshal(message, &request); err != nil {
		code = common.ParameterIllegal
		cmd = "error_on_controller"
		data = "error"
		fmt.Println("用户登录 解析数据失败", seq, err)
		return
	}
	db, err := database.Connect()
	if err != nil {
		code = common.ServerError
		return
	}
	cdb, err := database.Close(db)
	if err != nil {
		code = common.ServerError
		return
	}
	defer cdb.Close()
	repo := crud.NewRepositoryUsersCRUD(db)
	username := request["username"].(string)
	isFollowing := request["isFollowing"].(bool)
	user_with_following_info, err := repo.Get_User_with_Followings("username", username, isFollowing)
	if err != nil {
		code = common.UnauthorizedUserId
		cmd = "error_on_controller"
		data = "error"
		return
	}
	fmt.Printf("Fetched %s List Followings", username)
	console.Pretty(user_with_following_info, "")
	data = map[string]interface{}{ // Map literal
		"users":      user_with_following_info,
		"nextCursor": nil,
	}
	return
}
func GetBlockedFromRoomUsersController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("GetBlockedFromRoomUsersController")
	return
}
func GetMyFollowingsController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("GetMyFollowingController")
	code = common.OK
	voice = false
	cmd = "fetch_done"
	request := map[string]interface{}{"cursor": 0}
	if err := json.Unmarshal(message, &request); err != nil {
		code = common.ParameterIllegal
		cmd = "error_on_controller"
		data = "error"
		fmt.Println("用户登录 解析数据失败", seq, err)
		return
	}
	db, err := database.Connect()
	if err != nil {
		code = common.ServerError
		return
	}
	cdb, err := database.Close(db)
	if err != nil {
		code = common.ServerError
		return
	}
	if err != nil {
		code = common.ParameterIllegal
		cmd = "error_on_controller"
		data = "error"
		return
	}
	defer cdb.Close()
	repo := crud.NewRepositoryUsersCRUD(db)
	fmt.Println("details")
	user_with_following_info, err := repo.Get_User_with_Followings("user_id", client.UserId, false)
	if err != nil {
		code = common.UnauthorizedUserId
		cmd = "error_on_controller"
		data = "error"
		return
	}
	console.Pretty("Fetched my Followers", "")
	console.Pretty(user_with_following_info, "")
	data = map[string]interface{}{ // Map literal
		"users":      user_with_following_info,
		"nextCursor": nil,
	}
	// fmt.Println("Check data at GetMyFollowingController", data, preData)
	return
}
func GetTopPublicRoomsController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("GetTopPublicRoomsController")
	code = common.OK
	voice = true
	cmd = "fetch_done"
	request := &models.Room{}
	preData := request.TopRoomsPublicPrepData()
	data = map[string][]models.Room{ // Map literal
		"rooms": preData,
	}
	// fmt.Println("Check data at GetTopPublicRoomsController", data, preData)
	return
}
func GetUserProfileController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("GetUserProfileController")
	code = common.OK
	voice = false
	cmd = "fetch_done"
	request := map[string]string{"userId": ""}
	if err := json.Unmarshal(message, &request); err != nil {
		code = common.ParameterIllegal
		fmt.Println("用户登录 解析数据失败", seq, err)

		return
	}

	db, err := database.Connect()
	if err != nil {
		code = common.ServerError
		return
	}
	cdb, err := database.Close(db)
	if err != nil {
		code = common.ServerError
		return
	}
	defer cdb.Close()
	repo := crud.NewRepositoryUsersCRUD(db)
	user, err := repo.FindByUsername(request["userId"])
	if err != nil {
		code = common.UnauthorizedUserId
		cmd = "error_on_controller"
		data = "error"
		fmt.Println("用户登录 解析数据失败", seq, err)
		return
	}
	// if client.UserId != user.UserId {
	// 	add_follow_info := map[string]interface{}{
	// 		"following_info": user,
	// 	}
	// }
	data = user
	return
}
func GetScheduledRoomsController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("GetScheduledRoomsController")
	return
}
func GetRoomUsersController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("GetRoomUsersController")
	return
}

func SendRoomChatMsgController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("GetTopPublicRoomsController")
	code = common.OK
	voice = false
	cmd = "new_chat_msg"
	request := &models.RoomChatMsg{}
	request.ChatMsgPrepData()
	if err := json.Unmarshal(message, request); err != nil {
		code = common.ParameterIllegal
		cmd = "error_on_controller"
		data = "error"
		fmt.Println("用户登录 解析数据失败", seq, err)

		return
	}
	// fmt.Println("Printing json content", request)
	data = map[string]interface{}{ // Map literal
		"userId": "1",
		"msg":    request,
	}
	// data = map[string]map[string]interface{}{ // Map literal
	// 	"data": {"userId": "1", "msg": request},
	// }
	fmt.Println("Check msg resp data", data)
	return
}

func VoiceServerController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("GetTopPublicRoomsController")
	code = common.OK
	voice = false
	cmd = "you-joined-as-speaker"
	request := &models.RoomChatMsg{}
	request.ChatMsgPrepData()
	if err := json.Unmarshal(message, request); err != nil {
		code = common.ParameterIllegal
		cmd = "error_on_controller"
		data = "error"
		fmt.Println("用户登录 解析数据失败", seq, err)

		return
	}
	// fmt.Println("Printing json content", request)
	data = map[string]interface{}{ // Map literal
		"userId": "1",
		"msg":    request,
	}
	// data = map[string]map[string]interface{}{ // Map literal
	// 	"data": {"userId": "1", "msg": request},
	// }
	fmt.Println("Check msg resp data", data)
	return
}

func WTFController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("WTFController")
	return
}

func EditProfileController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("EditProfileController")
	code = common.OK
	voice = false
	cmd = "fetch_done"
	request := map[string]interface{}{"data": models.User{}}
	if err := json.Unmarshal(message, &request); err != nil {
		code = common.ParameterIllegal
		fmt.Println("用户登录 解析数据失败", seq, err)

		return
	}

	db, err := database.Connect()
	if err != nil {
		code = common.ServerError
		return
	}
	cdb, err := database.Close(db)
	if err != nil {
		code = common.ServerError
		return
	}
	defer cdb.Close()
	repo := crud.NewRepositoryUsersCRUD(db)
	user, err := repo.FindByUsername(request["data"].(map[string]interface{})["username"].(string))
	if err != nil {
		code = common.UnauthorizedUserId
		cmd = "error_on_controller"
		data = "error"
		fmt.Println("用户登录 解析数据失败", seq, err)
		return
	}
	user.DisplayName = request["data"].(map[string]interface{})["displayName"].(string)
	user.Bio = request["data"].(map[string]interface{})["bio"].(string)
	user.Avatar = request["data"].(map[string]interface{})["avatarUrl"].(string)

	_, err = repo.Update3(request["data"].(map[string]interface{})["username"].(string), user)
	if err != nil {
		code = common.UnauthorizedUserId
		cmd = "error_on_controller"
		data = "error"
		fmt.Println("用户登录 解析数据失败", seq, err)
		return
	}
	resp_obj := map[string]interface{}{
		"isUsernameTaken": false,
	}
	data = resp_obj
	return
}

// --------------------------------- Marketplace controllers ---------------------------------
func GetMkPublicationController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("GetMkPublicationController")
	code = common.OK
	voice = true
	cmd = "fetch_done"
	request := map[string]interface{}{"publicationId": ""}
	if err := json.Unmarshal(message, &request); err != nil {
		code = common.ParameterIllegal
		fmt.Println("用户登录 解析数据失败", seq, err)

		return
	}

	db, err := database.Connect()
	if err != nil {
		code = common.ServerError
		return
	}
	cdb, err := database.Close(db)
	if err != nil {
		code = common.ServerError
		return
	}
	defer cdb.Close()
	repo := crud.NewRepositoryMarketplaceCRUD(db)
	err = repo.FindPublicationById(request)
	if err != nil {
		code = common.ServerError
		data = map[string]error{"error": err}
		return
	}
	data = &request
	return
}
func GetMkListPublicationController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("GetMkListPublicationController")
	code = common.OK
	voice = true
	cmd = "fetch_done"
	request := map[string]interface{}{"cursor": 0}
	if err := json.Unmarshal(message, &request); err != nil {
		code = common.ParameterIllegal
		fmt.Println("用户登录 解析数据失败", seq, err)

		return
	}

	db, err := database.Connect()
	if err != nil {
		code = common.ServerError
		return
	}
	cdb, err := database.Close(db)
	if err != nil {
		code = common.ServerError
		return
	}
	defer cdb.Close()
	repo := crud.NewRepositoryMarketplaceCRUD(db)
	err = repo.GetInitialPublicationList(request)
	if err != nil {
		code = common.ServerError
		return
	}
	data = &request
	// fmt.Println("Checking after query method", request, &request)
	return
}
func CreateMarketplacePublicationController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("CreateMarketplacePublicationController")
	code = common.OK
	voice = true
	cmd = "fetch_done"
	request := map[string]interface{}{
		"title":        "",
		"price":        "",
		"category":     "",
		"state":        "",
		"description":  "",
		"availability": "",
		"brand":        "",
		"labels":       "",
		"sku":          "",
		"formData":     "",
	}

	fmt.Println("check message as string: ", string(message))
	if err := json.Unmarshal(message, &request); err != nil {
		code = common.ParameterIllegal
		fmt.Println("用户登录 解析数据失败", seq, err)

		return
	}
	publication := &models.Publication{}
	err := publication.Validate(request)
	if err != nil {
		code = common.ParameterIllegal
		data = "error"
		fmt.Println("用户登录 解析数据失败", seq, err)
		return
	}
	fmt.Println("check request[formData]: ", request["formData:map"])
	// models.Validate()
	// resp, err := http.Get("http://localhost:3000/c888cf5d-adb3-4139-bc06-5f72badf3434")
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// defer resp.Body.Close()

	// // Read the response body as a byte slice
	// bytes, err := ioutil.ReadAll(resp.Body)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// mimeType1 := http.DetectContentType(bytes)
	// fmt.Println("mimeType1: ", mimeType1)
	// mimeType2 := http.DetectContentType(request.formData)
	// fmt.Println("mimeType2: ", mimeType2) // image/png
	// md, _ := request["formData"].(map[string]interface{})
	// fmt.Println(md)
	// if md == nil {
	// 	fmt.Println("empty")
	// }
	// fmt.Println(len(md))
	// request["formData"], err := ioutil.ReadFile("myImage")
	// if err != nil {
	// 	log.Fatalf("unable to read file: %v", err)
	// }
	// fmt.Println(string(body))
	// form := request["formData"]
	// file, err := http.Request(form)
	// if err != nil {
	// 	fmt.Println(http.ResponseWriter, "%v", err)
	// 	return
	// }
	// fmt.Println("check file content", file)
	// p, err := ioutil.ReadFile(form)
	// if err != nil {
	// 	fmt.Println("Error on reading file", err)
	// }
	// fmt.Println("post publication content: ", request, request["formData"], p)

	db, err := database.Connect()
	if err != nil {
		code = common.ServerError
		return
	}
	cdb, err := database.Close(db)
	if err != nil {
		code = common.ServerError
		return
	}
	fmt.Println("Checking data before DB", publication)
	defer cdb.Close()
	repo := crud.NewRepositoryMarketplaceCRUD(db)
	err = repo.CreatePublication(*publication)
	if err != nil {
		code = common.ServerError
		return
	}
	data = map[string]string{"resp": "ok"}
	// fmt.Println("Checking after query method", request, &request)
	return
}

func EditeMarketplacePublicationController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("EditeMarketplacePublicationController")
	code = common.OK
	voice = true
	cmd = "fetch_done"
	request := map[string]interface{}{"cursor": 0}
	if err := json.Unmarshal(message, &request); err != nil {
		code = common.ParameterIllegal
		fmt.Println("用户登录 解析数据失败", seq, err)

		return
	}

	db, err := database.Connect()
	if err != nil {
		code = common.ServerError
		return
	}
	cdb, err := database.Close(db)
	if err != nil {
		code = common.ServerError
		return
	}
	defer cdb.Close()
	repo := crud.NewRepositoryMarketplaceCRUD(db)
	err = repo.GetInitialPublicationList(request)
	if err != nil {
		code = common.ServerError
		return
	}
	data = &request
	// fmt.Println("Checking after query method", request, &request)
	return
}

// --------------------------------- Mutation controllers ---------------------------------
func UserUpdateController(client *Client, seq string, icmd string, message []byte) {
	fmt.Println("UserUpdateController")
}
func RoomUpdateController(client *Client, seq string, icmd string, message []byte) {
	fmt.Println("RoomUpdateController")
}
func FollowController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("FollowController")
	fmt.Println("FollowController params passed:", string(message))
	code = common.OK
	voice = false
	cmd = "fetch_done"
	request := map[string]interface{}{"userId": "", "value": false}
	if err := json.Unmarshal(message, &request); err != nil {
		code = common.ParameterIllegal
		cmd = "error_on_controller"
		data = "error"
		fmt.Println("用户登录 解析数据失败", seq, err)
		return
	}
	db, err := database.Connect()
	if err != nil {
		code = common.ServerError
		return
	}
	cdb, err := database.Close(db)
	if err != nil {
		code = common.ServerError
		return
	}
	if err != nil {
		code = common.ParameterIllegal
		cmd = "error_on_controller"
		data = "error"
		return
	}
	defer cdb.Close()
	repo := crud.NewRepositoryUsersCRUD(db)
	userId := request["userId"].(string)
	follow := request["value"].(bool)
	user_with_following_info, err := repo.UpdateFollowState(client.UserId, userId, follow)
	if err != nil {
		code = common.UnauthorizedUserId
		cmd = "error_on_controller"
		data = "error"
		return
	}
	console.Pretty("Fetched my Followers", "")
	console.Pretty(user_with_following_info, "")
	data = map[string]interface{}{ // Map literal
		"users":      user_with_following_info,
		"nextCursor": nil,
	}
	// fmt.Println("Check data at GetMyFollowingController", data, preData)
	return
}
func UserUpdate2Controller(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("UserUpdate2Controller")
	return
}
func UserUpdateBlockController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("UserUpdateBlockController")
	return
}
func UserUpdateUnblockController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("UserUpdateUnblockController")
	return
}
func RoomUpdate2Controller(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("RoomUpdate2Controller")
	return
}
func RoomUpdateBanController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("RoomUpdateBanController")
	return
}
func RoomUpdateDeafenController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("RoomUpdateDeafenController")
	return
}
func DeleteScheduledRoomController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("DeleteScheduledRoomController")
	return
}
func CreateRoomFromScheduledRoomController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("CreateRoomFromScheduledRoomController")
	return
}
func ScheduleRoomController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("ScheduleRoomController")
	return
}
func EditScheduledRoomController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("EditScheduledRoomController")
	return
}
func AskToSpeakController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("AskToSpeakController")
	return
}
func InviteToRoomController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("InviteToRoomController")
	return
}
func SpeakingChangeController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("SpeakingChangeController")
	return
}
func BanFromRoomController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("BanFromRoomController")
	return
}
func ChangeModStatusController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("ChangeModStatusController")
	return
}
func ChangeRoomCreatorController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("ChangeRoomCreatorController")
	return
}
func AddSpeakerController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("AddSpeakerController")
	return
}
func DeleteRoomChatMessageController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("DeleteRoomChatMessageController")
	return
}
func UnbanFromRoomChatController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("UnbanFromRoomChatController")
	return
}
func BanFromRoomChatController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("BanFromRoomChatController")
	return
}
func SetListenerController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("SetListenerController")
	return
}
func MuteController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("MuteController")
	return
}
func LeaveRoomController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("LeaveRoomController")
	return
}
func CreateRoomController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("CreateRoomController")
	return
}
func EditeRoomController(client *Client, seq string, icmd string, message []byte) (code uint32, msg string, cmd string, data interface{}, voice bool) {
	fmt.Println("EditeRoomController")
	return
}

// defp dispatch_mediasoup_message(msg, %{user: %{id: user_id}}) do
// with {:ok, room_id} <- Beef.Users.tuple_get_current_room_id(user_id),
// 	 [{_, _}] <- Onion.RoomSession.lookup(room_id) do
//   voice_server_id = Onion.RoomSession.get(room_id, :voice_server_id)

//   mediasoup_message =
// 	msg
// 	|> Map.put("d", msg["p"] || msg["d"])
// 	|> put_in(["d", "peerId"], user_id)
// 	# voice server expects this key
// 	|> put_in(["uid"], user_id)
// 	|> put_in(["d", "roomId"], room_id)

//   Onion.VoiceRabbit.send(voice_server_id, mediasoup_message)
// end

// # if this results in something funny because the user isn't in a room, we
// # will just swallow the result, it means that there is some amount of asynchrony
// # in the information about who is in what room.
// end

// 用户登录
// func LoginController(client *Client, seq string, message []byte) (code uint32, msg string, data interface{}, voice bool) {

// 	code = common.OK
// 	currentTime := uint64(time.Now().Unix())

// 	request := &models.Login{}
// 	if err := json.Unmarshal(message, request); err != nil {
// 		code = common.ParameterIllegal
// 		fmt.Println("用户登录 解析数据失败", seq, err)

// 		return
// 	}

// 	fmt.Println("webSocket_request 用户登录", seq, "ServiceToken", request.ServiceToken)

// 	// TODO::进行用户权限认证，一般是客户端传入TOKEN，然后检验TOKEN是否合法，通过TOKEN解析出来用户ID
// 	// 本项目只是演示，所以直接过去客户端传入的用户ID
// 	if request.UserId == "" || len(request.UserId) >= 20 {
// 		code = common.UnauthorizedUserId
// 		fmt.Println("用户登录 非法的用户", seq, request.UserId)

// 		return
// 	}

// 	if !InAppIds(request.AppId) {
// 		code = common.Unauthorized
// 		fmt.Println("用户登录 不支持的平台", seq, request.AppId)

// 		return
// 	}

// 	if client.IsLogin() {
// 		fmt.Println("用户登录 用户已经登录", client.AppId, client.UserId, seq)
// 		code = common.OperationFailure

// 		return
// 	}

// 	client.Login(request.AppId, request.UserId, currentTime)

// 	// 存储数据
// 	userOnline := models.UserLogin(serverIp, serverPort, request.AppId, request.UserId, client.Addr, currentTime)
// 	err := cache.SetUserOnlineInfo(client.GetKey(), userOnline)
// 	if err != nil {
// 		code = common.ServerError
// 		fmt.Println("用户登录 SetUserOnlineInfo", seq, err)

// 		return
// 	}

// 	// 用户登录
// 	login := &login{
// 		AppId:  request.AppId,
// 		UserId: request.UserId,
// 		Client: client,
// 	}
// 	clientManager.Login <- login

// 	fmt.Println("用户登录 成功", seq, client.Addr, request.UserId)

// 	return
// }

// // 心跳接口
// func HeartbeatController(client *Client, seq string, message []byte) (code uint32, msg string, data interface{}, voice bool) {

// 	code = common.OK
// 	currentTime := uint64(time.Now().Unix())

// 	request := &models.HeartBeat{}
// 	if err := json.Unmarshal(message, request); err != nil {
// 		code = common.ParameterIllegal
// 		fmt.Println("心跳接口 解析数据失败", seq, err)

// 		return
// 	}

// 	fmt.Println("webSocket_request 心跳接口", client.AppId, client.UserId)

// 	if !client.IsLogin() {
// 		fmt.Println("心跳接口 用户未登录", client.AppId, client.UserId, seq)
// 		code = common.NotLoggedIn

// 		return
// 	}

// 	userOnline, err := cache.GetUserOnlineInfo(client.GetKey())
// 	if err != nil {
// 		if err == redis.Nil {
// 			code = common.NotLoggedIn
// 			fmt.Println("心跳接口 用户未登录", seq, client.AppId, client.UserId)

// 			return
// 		} else {
// 			code = common.ServerError
// 			fmt.Println("心跳接口 GetUserOnlineInfo", seq, client.AppId, client.UserId, err)

// 			return
// 		}
// 	}

// 	client.Heartbeat(currentTime)
// 	userOnline.Heartbeat(currentTime)
// 	err = cache.SetUserOnlineInfo(client.GetKey(), userOnline)
// 	if err != nil {
// 		code = common.ServerError
// 		fmt.Println("心跳接口 SetUserOnlineInfo", seq, client.AppId, client.UserId, err)

// 		return
// 	}

// 	return
// }
