package main

import (
	filesystem "awesomeProject1/filesystem"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/websocket"
)

type onlineDate struct {
	Id         int    `json:"id"`
	DeviceType int    `json:"deviceType"`
	Username   string `json:"userName"`
	connection *websocket.Conn
} //system

var onlineUsers = make(map[int]onlineDate) //在线id

var upgrade = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
} //system

type Message struct {
	Command string      `json:"command"`
	Content interface{} `json:"content"`
} //system

func sendJSON(conn *websocket.Conn, message interface{}) {
	jsonData, err := json.Marshal(message)
	if err != nil {
		log.Println("JSON marshal error:", err)
		return
	}

	err = conn.WriteMessage(websocket.TextMessage, jsonData)
	if err != nil {
		log.Println("Write message error:", err)
		return
	}
}

func wsProcessor(w http.ResponseWriter, r *http.Request) {
	upgrade.CheckOrigin = func(r *http.Request) bool { return true }
	ws, err := upgrade.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("Client Connected to Endpoint...")
	reader(ws)
} //system

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
	// 遍历查询结果并发送消息到 WebSocket 连接
	for rows.Next() {
		var message Message
		err := rows.Scan(&message.ID, &message.Sender, &message.Recipient, &message.Content, &message.Timestamp)
		if err != nil {
			return err
		}

		// 发送消息到 WebSocket 连接
		sendJSON(onlineUsers[recipient].connection, message)

		// 删除数据库中的相应行
		err = deleteMessageFromDB(db, message.ID)
		if err != nil {
			return err
		}
	}
	return nil
}

func deleteMessageFromDB(db *sql.DB, messageID int) error {
	// 执行删除操作
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
	//init
	thisUser.loginState = false
	for {
		_, p, err := conn.ReadMessage()
		if err != nil {
			fmt.Println(err)
			return
		}

		var msg Message
		err = json.Unmarshal(p, &msg)
		if err != nil {
			fmt.Println(err)
			return
		}

		fmt.Printf("Received: %v\n", msg)
		params := msg.Content.(map[string]interface{})
		if thisUser.loginState == false {
			switch msg.Command {
			case "login":
				userID, ok := params["userId"].(float64)
				if !ok {
					sendJSON(thisUser.connection, map[string]interface{}{"command": "login", "status": "error", "message": "invalid params"})
					continue
				}
				thisUser.userId = int(userID)

				deviceType, ok := params["deviceType"].(float64)
				if !ok {
					sendJSON(thisUser.connection, map[string]interface{}{"command": "login", "status": "error", "message": "invalid params"})
					continue
				}
				thisUser.deviceType = int(deviceType)

				password, ok := params["password"].(string)
				if !ok {
					sendJSON(thisUser.connection, map[string]interface{}{"command": "login", "status": "error", "message": "invalid params"})
					continue
				}

				var storedPassword string
				query := "SELECT userPassword,userName,tag FROM UserBasicData WHERE userID=?"
				err := db.QueryRow(query, thisUser.userId).Scan(&storedPassword, &thisUser.userName, &thisUser.tag)
				switch {
				case errors.Is(err, sql.ErrNoRows):
					fmt.Println("用户不存在")
					sendJSON(thisUser.connection, map[string]interface{}{"command": "login", "status": "error", "message": "用户不存在"})
				case err != nil:
					fmt.Println("查询错误:", err)
					sendJSON(thisUser.connection, map[string]interface{}{"command": "login", "status": "error", "message": fmt.Sprintf("查询错误:%v", err)})
				default:
					// 检查密码是否匹配
					if storedPassword == password {
						fmt.Println("登录成功")
						sendJSON(thisUser.connection, map[string]interface{}{"command": "login", "status": "success", "message": "登录成功", "userId": thisUser.userId, "userName": thisUser.userName, "userTag": thisUser.tag})
						thisUser.loginState = true
						onlineUsers[thisUser.userId] = onlineDate{thisUser.userId, thisUser.deviceType, thisUser.userName, thisUser.connection}
						err := sendMessageFromDB(thisUser.userId)
						if err != nil {
							fmt.Println("拉取缓存错误：", err)
							sendJSON(thisUser.connection, map[string]interface{}{"command": "login", "status": "error", "message": "拉取缓存错误"})
							return
						}
					} else {
						fmt.Println("密码不匹配")
						sendJSON(thisUser.connection, map[string]interface{}{"command": "login", "status": "error", "message": "密码不匹配"})
					}
				}

			case "register":
				userName, ok := params["userName"].(string)
				if !ok {
					sendJSON(conn, map[string]interface{}{"command": "register", "status": "error", "message": "invalid userName"})
					return
				}

				deviceType, ok := params["deviceType"].(float64)
				if !ok {
					sendJSON(conn, map[string]interface{}{"command": "register", "status": "error", "message": "invalid deviceType"})
					return
				}

				userPassword, ok := params["userPassword"].(string)
				if !ok {
					sendJSON(conn, map[string]interface{}{"command": "register", "status": "error", "message": "invalid userPassword"})
					return
				}

				// 使用userName和deviceType插入记录
				result, err := db.Exec("INSERT INTO UserBasicData (userName, deviceType, userPassword, teachingClass) VALUES (?, ?, ?, ?)", userName, int(deviceType), userPassword, "[]")
				if err != nil {
					fmt.Println("插入记录失败:", err)
					sendJSON(conn, map[string]interface{}{"command": "register", "status": "error", "message": fmt.Sprintf("插入记录失败:%v", err)})
					return
				}

				// 获取插入的递增ID
				id, _ := result.LastInsertId()
				fmt.Printf("用户 %s 插入成功，ID：%d\n", userName, id)
				sendJSON(conn, map[string]interface{}{"command": "register", "status": "success", "message": fmt.Sprintf("用户 %s 插入成功，ID：%d", userName, id)})

				// 插入tag
				teacherTag, ok := params["tag"].(string)
				if ok && teacherTag != "" {
					_, err := db.Exec("UPDATE UserBasicData SET tag = ? WHERE userID = ?", teacherTag, id)
					if err != nil {
						fmt.Println("插入tag失败:", err)
						sendJSON(conn, map[string]interface{}{"command": "register", "status": "error", "message": fmt.Sprintf("插入tag失败:%v", err)})
						return
					}
					fmt.Printf("tag: %s\n", teacherTag)
					sendJSON(conn, map[string]interface{}{"command": "register", "status": "success", "message": fmt.Sprintf("tag: %s", teacherTag)})
				}
			default:
				fmt.Println("Permission denied:", msg.Command)
				sendJSON(conn, map[string]interface{}{"command": "register", "status": "error", "message": "Permission denied"})
			}
		} else {
			switch msg.Command {
			case "logout":
				delete(onlineUsers, thisUser.userId)
				sendJSON(conn, map[string]interface{}{"command": "logout", "status": "success", "message": "done"})

			case "getOnlineUser":
				userID, ok := params["id"].(float64)
				if !ok {
					sendJSON(conn, map[string]interface{}{"command": "getOnlineUser", "status": "error", "message": "invalid id"})
					return
				}

				selectrdUser, online := onlineUsers[int(userID)]
				var userName string
				if online {
					userName = selectrdUser.Username
				} else {
					query := "SELECT userName FROM UserBasicData WHERE userID=?"
					err := db.QueryRow(query, thisUser.userId).Scan(userName)
					if err != nil {
						sendJSON(conn, map[string]interface{}{"command": "getOnlineUser", "status": "error", "message": "数据库查询错误"})
						return
					}
				}
				sendJSON(conn, map[string]interface{}{"command": "getOnlineUser", "status": "success", "result": online, "userId": userID, "userName": userName})
			case "getTeachingClasses":
				rows, err := db.Query("SELECT teachingClass FROM UserBasicData WHERE userID = ?", thisUser.userId)
				if err != nil {
					fmt.Println("查询班级信息失败:", err)
					sendJSON(conn, map[string]interface{}{"command": "getTeachingClasses", "status": "error", "message": fmt.Sprintf("查询班级信息失败:%v", err)})
					return
				}
				defer rows.Close()

				// 读取查询结果
				var teachingClass string
				if rows.Next() {
					if err := rows.Scan(&teachingClass); err != nil {
						fmt.Println("扫描班级信息失败:", err)
						sendJSON(conn, map[string]interface{}{"command": "getTeachingClasses", "status": "error", "message": fmt.Sprintf("扫描班级信息失败:%v", err)})
						return
					}
				}

				// 发送教学班级信息给客户端
				sendJSON(conn, map[string]interface{}{"command": "getTeachingClasses", "status": "success", "teachingClass": teachingClass})
			case "addClass":
				addingClass, ok := params["class"].(string)
				if !ok {
					sendJSON(conn, map[string]interface{}{"command": "addClass", "status": "error", "message": "invalid class"})
					return
				}

				_, err = db.Exec("UPDATE UserBasicData SET teachingClass = JSON_ARRAY_APPEND(teachingClass, '$', ?) WHERE userID = ?", addingClass, thisUser.userId)
				if err != nil {
					fmt.Println("添加失败：", err)
					sendJSON(conn, map[string]interface{}{"command": "addClass", "status": "error", "message": fmt.Sprintf("添加失败：%v", err)})
				} else {
					fmt.Println("添加班级成功")
					sendJSON(conn, map[string]interface{}{"command": "addClass", "status": "success", "message": "添加班级成功"})
				}
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
						// 处理错误
						fmt.Println("Error inserting data:", err)
						sendJSON(conn, map[string]interface{}{"command": "sendMessage", "status": "error", "message": fmt.Sprintln("Error inserting data:", err)})
						return
					} else {
						responseMessage = "不在线，进入缓存"
					}
				} else {
					recipientConnection := onlineUsers[recipientID].connection
					sendJSON(recipientConnection, map[string]interface{}{"command": "receiveMessage", "message": message})
					responseMessage = "已发送"
				}

				sendJSON(conn, map[string]interface{}{"command": "sendMessage", "status": "success", "message": responseMessage})
			default:
				fmt.Println("Unknown command:", msg.Command)
				sendJSON(conn, map[string]interface{}{"command": msg.Command, "status": "error", "message": "Unknown command"})
			}
		}
	}
}

func setupRoutes() {
	http.HandleFunc("/ws", wsProcessor)
	// 设置文件上传和下载的路由
	http.HandleFunc("/upload", filesystem.HandleFileUpload)
	http.HandleFunc("/download/", filesystem.HandleFileDownload)
} //system

var db *sql.DB

func init() {
	// 初始化数据库连接
	var err error
	db, err = sql.Open("mysql", "root:12345678@tcp(localhost:3306)/skyflysyncdb")
	if err != nil {
		fmt.Println("数据库连接失败:", err)
		return
	}
}

func main() {
	fmt.Println("Go WebSocket")
	setupRoutes()
	err := http.ListenAndServe(":8900", nil)
	if err != nil {
		fmt.Printf("http.ListenAndServe: %v", err)
	}

	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(db) //system
}
