package main

import (
	filesystem "SkyFlySyncServer/filesystem"
	"config"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"logger"
	"net/http"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/websocket"
)

type onlineData struct {
	Id         int    `json:"id"`
	DeviceType int    `json:"deviceType"`
	Username   string `json:"userName"`
	connection *websocket.Conn
}

var onlineUsers = make(map[int]onlineData) //在线id

var upgrade = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type Message struct {
	Command string      `json:"command"`
	Content interface{} `json:"content"`
}

func sendJSON(conn *websocket.Conn, message interface{}) {
	jsonData, err := json.Marshal(message)
	if err != nil {
		logger.Error("JSON转码错误:", err)
		return
	}
	logger.Info("用户发送消息，转发包体为:", err)
	err = conn.WriteMessage(websocket.TextMessage, jsonData)
	if err != nil {
		logger.Error("发送消息出现错误:", err)
		return
	}
}

func sendBackDataPack(conn *websocket.Conn, command string, status string, message string, content interface{}) {
	response := map[string]interface{}{
		"command": command,
		"status":  status,
		"message": message,
		"content": content,
	}
	sendJSON(conn, response)
}

func wsProcessor(w http.ResponseWriter, r *http.Request) {
	upgrade.CheckOrigin = func(r *http.Request) bool { return true }
	ws, err := upgrade.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("WebSocket升级错误:", err)
	}
	logger.Info("新的客户端已连接")
	reader(ws)
}

func sendMessageFromDB(recipient int) error {
	rows, err := db.Query("SELECT id, sender, recipient, content, timestamp FROM messagedb WHERE recipient = ?", recipient)
	if err != nil {
		return err
	}
	type Message struct {
		ID        int    `json:"id"`
		Sender    int    `json:"sender"`
		Recipient int    `json:"recipient"`
		Content   string `json:"content"`
		Timestamp string `json:"timestamp"`
	}
	for rows.Next() {
		var message Message
		err := rows.Scan(&message.ID, &message.Sender, &message.Recipient, &message.Content, &message.Timestamp)
		if err != nil {
			return err
		}

		sendJSON(onlineUsers[recipient].connection, message)

		err = deleteMessageFromDB(db, message.ID)
		if err != nil {
			return err
		}
	}
	return nil
}

func deleteMessageFromDB(db *sql.DB, messageID int) error {
	_, err := db.Exec("DELETE FROM messagedb WHERE id = ?", messageID)
	return err
}

func reader(conn *websocket.Conn) {
	thisUser := struct {
		userId     int
		userName   string
		deviceType int
		loginState bool
		tag        string
		connection *websocket.Conn
	}{
		connection: conn,
	}

	thisUser.loginState = false
	for {
		_, p, err := conn.ReadMessage()
		if err != nil {
			logger.Error(err)
			return
		}

		var msg Message
		err = json.Unmarshal(p, &msg)
		if err != nil {
			logger.Error(err)
			return
		}

		logger.Info("收到消息:", msg)
		params := msg.Content.(map[string]interface{})
		if thisUser.loginState == false {
			switch msg.Command {
			case "login":
				userID, ok := params["userId"].(float64)
				password, ok := params["userPassword"].(string)
				deviceType, ok := params["deviceType"].(float64)
				if !ok {
					sendBackDataPack(thisUser.connection, "login", "error", "无效的参数", nil)
					continue
				}
				thisUser.userId = int(userID)
				thisUser.deviceType = int(deviceType)
				var storedPassword string
				query := "SELECT userPassword,userName,tag FROM UserBasicData WHERE userID=?"
				err := db.QueryRow(query, thisUser.userId).Scan(&storedPassword, &thisUser.userName, &thisUser.tag)
				switch {
				case errors.Is(err, sql.ErrNoRows):
					logger.Info("用户不存在")
					sendBackDataPack(thisUser.connection, "login", "error", "用户不存在", nil)
				case err != nil:
					logger.Info("查询错误:", err)
					sendBackDataPack(thisUser.connection, "login", "error", fmt.Sprintf("查询错误:%v", err), nil)
				default:
					if storedPassword == password {
						logger.Info("登录成功")
						sendBackDataPack(thisUser.connection, "login", "success", "登录成功", map[string]interface{}{"userId": thisUser.userId, "userName": thisUser.userName, "userTag": thisUser.tag})
						thisUser.loginState = true
						onlineUsers[thisUser.userId] = onlineData{thisUser.userId, thisUser.deviceType, thisUser.userName, thisUser.connection}

					} else {
						logger.Info("密码不匹配", storedPassword, ":", password)
						sendBackDataPack(thisUser.connection, "login", "error", "密码不匹配", nil)
					}
				}
			case "getOfflineData":
				err := sendMessageFromDB(thisUser.userId)
				if err != nil {
					logger.Info("拉取缓存错误：", err)
					sendBackDataPack(thisUser.connection, "login", "error", "拉取缓存错误", nil)
					return
				}
			case "register":
				userName, ok := params["userName"].(string)
				if !ok {
					sendBackDataPack(conn, "register", "error", "无效的用户名", nil)
					return
				}

				deviceType, ok := params["deviceType"].(float64)
				if !ok {
					sendBackDataPack(conn, "register", "error", "无效的设备类型", nil)
					return
				}

				userPassword, ok := params["userPassword"].(string)
				if !ok {
					sendBackDataPack(conn, "register", "error", "无效的密码", nil)
					return
				}

				userTag, ok := params["userTag"].(string)
				var result sql.Result
				if !ok {
					userTag = "无标签"
				}
				result, err = db.Exec("INSERT INTO UserBasicData (userName, deviceType, userPassword, teachingClass, tag) VALUES (?, ?, ?, ?, ?)", userName, int(deviceType), userPassword, "[]", userTag)
				if err != nil {
					logger.Error("插入记录失败:", err)
					sendBackDataPack(conn, "register", "error", "插入记录失败", nil)
					return
				}

				id, _ := result.LastInsertId()
				sendBackDataPack(conn, "register", "success", fmt.Sprintf("用户 %s 插入成功，ID：%d", userName, id), map[string]interface{}{"userName": userName, "userId": id})

				teacherTag, ok := params["tag"].(string)
				if ok && teacherTag != "" {
					_, err := db.Exec("UPDATE UserBasicData SET tag = ? WHERE userID = ?", teacherTag, id)
					if err != nil {
						logger.Info("插入tag失败:", err)
						sendBackDataPack(conn, "register", "error", fmt.Sprintf("插入tag失败:%v", err), nil)
						return
					}
					logger.Info("tag: %s", teacherTag)
					sendBackDataPack(conn, "register", "success", fmt.Sprintf("tag: %s", teacherTag), nil)
				}

			default:
				logger.Info("无权限执行命令:", msg.Command)
				sendBackDataPack(conn, msg.Command, "error", "无权限执行命令", nil)
			}
		} else {
			switch msg.Command {
			case "logout":
				delete(onlineUsers, thisUser.userId)
				sendBackDataPack(conn, "logout", "success", "完成", nil)
			case "getOnlineUser":
				userID, ok := params["userId"].(float64)
				if !ok {
					sendBackDataPack(conn, "getOnlineUser", "error", "无效的id", nil)
					return
				}

				selectedUser, online := onlineUsers[int(userID)]
				var userName string
				if online {
					userName = selectedUser.Username
				} else {
					query := "SELECT username FROM UserBasicData WHERE userId=?"
					err := db.QueryRow(query, int(userID)).Scan(userName)
					if err != nil {
						sendJSON(conn, map[string]interface{}{"command": "getOnlineUser", "status": "error", "message": "数据库查询错误"})
						return
					}
				}
				sendBackDataPack(conn, "getOnlineUser", "success", "", map[string]interface{}{"online": online, "userId": userID, "userName": userName})
			case "getTeachingClasses":
				rows, err := db.Query("SELECT teachingClass FROM UserBasicData WHERE userID = ?", thisUser.userId)
				if err != nil {
					logger.Info("查询班级信息失败:", err)
					sendJSON(conn, map[string]interface{}{"command": "getTeachingClasses", "status": "error", "message": fmt.Sprintf("查询班级信息失败:%v", err)})
					return
				}
				defer rows.Close()

				var teachingClass string
				if rows.Next() {
					if err := rows.Scan(&teachingClass); err != nil {
						logger.Info("扫描班级信息失败:", err)
						sendJSON(conn, map[string]interface{}{"command": "getTeachingClasses", "status": "error", "message": fmt.Sprintf("扫描班级信息失败:%v", err)})
						return
					}
				}

				sendJSON(conn, map[string]interface{}{"command": "getTeachingClasses", "status": "success", "teachingClass": teachingClass})
			case "addClass":
				addingClass, ok := params["class"].(string)
				if !ok {
					sendJSON(conn, map[string]interface{}{"command": "addClass", "status": "error", "message": "无效的班级"})
					return
				}

				_, err = db.Exec("UPDATE UserBasicData SET teachingClass = JSON_ARRAY_APPEND(teachingClass, '$', ?) WHERE userID = ?", addingClass, thisUser.userId)
				if err != nil {
					logger.Info("添加失败：", err)
					sendJSON(conn, map[string]interface{}{"command": "addClass", "status": "error", "message": fmt.Sprintf("添加失败：%v", err)})
				} else {
					logger.Info("添加班级成功")
					sendJSON(conn, map[string]interface{}{"command": "addClass", "status": "success", "message": "添加班级成功"})
				}
			case "deleteClass":
				//TODO
			case "sendMessage":
				var responseMessage string
				var recipientID = int(params["recipient"].(float64))
				_, recipientOnline := onlineUsers[recipientID]

				var message = struct {
					SenderName string
					SenderID   int
					Content    interface{}
					Time       time.Time
				}{
					SenderName: thisUser.userName,
					SenderID:   thisUser.userId,
					Content:    params["content"],
					Time:       time.Now(),
				}

				if !recipientOnline {
					_, err := db.Exec("INSERT INTO messagedb (sender, recipient, content, timestamp) VALUES (?, ?, ?, ?)", message.SenderID, recipientID, message.Content, message.Time)
					if err != nil {
						logger.Info("插入数据错误:", err)
						sendJSON(conn, map[string]interface{}{"command": "sendMessage", "status": "error", "message": fmt.Sprintln("插入数据错误:", err)})
						return
					} else {
						responseMessage = "不在线，进入缓存"
					}
				} else {
					recipientConnection := onlineUsers[recipientID].connection
					sendJSON(recipientConnection, map[string]interface{}{"command": msg.Command, "message": message, "content": msg.Content})
					responseMessage = "已发送"
				}

				sendJSON(conn, map[string]interface{}{"command": msg.Command, "status": "success", "message": responseMessage})
			default:
				logger.Info("未知命令:", msg.Command)
				sendJSON(conn, map[string]interface{}{"command": msg.Command, "status": "error", "message": "未知命令"})
			}
		}
	}
}

func setupRoutes() {
	http.HandleFunc(Config.WebSocketServiceRote, wsProcessor)
	http.HandleFunc(Config.UploadServiceRote, filesystem.HandleFileUpload)
	http.HandleFunc(Config.DownloadServiceRote, filesystem.HandleFileDownload)
}

var db *sql.DB

var Config config.Config

func init() {
	// 初始化数据库连接
	var err error
	Config, err = config.LoadConfig("Config.json")
	if err != nil {
		logger.Error("读取配置文件错误:", err)
		return
	}
	logger.SetLogLevel(logger.LogLevel(Config.LogLevel))
	logger.Info("日志等级被设置为config.LogLevel")

	// 初始化数据库连接
	db, err = sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s)/skyflysyncdb", Config.DataBaseSettings.Account, Config.DataBaseSettings.Password, Config.DataBaseSettings.Address))
	if err != nil {
		logger.Error("Database connection failed:", err)
		return
	}
}
func main() {
	setupRoutes()
	_PORT := ":8900"
	logger.Info("服务器启动成功！端口", _PORT)
	err := http.ListenAndServe(_PORT, nil)
	if err != nil {
		logger.Error("http.ListenAndServe: %v", err)
	}
	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			logger.Error(err)
		}
	}(db)
}
