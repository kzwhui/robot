package main

import (
	"flag"
	"fmt"
	"github.com/evolsnow/robot/conn"
	"github.com/gorilla/websocket"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
	return true
}} // use default options for webSocket

func main() {
	var configFile string
	var debug bool

	flag.StringVar(&configFile, "c", "config.json", "specify config file")
	flag.BoolVar(&debug, "d", false, "debug mode")
	flag.Parse()
	config, err := ParseConfig(configFile)
	if err != nil {
		log.Fatal("a vailid json config file must exist")
	}

	//connect to redis
	redisPort := strconv.Itoa(config.RedisPort)
	redisServer := net.JoinHostPort(config.RedisAddress, redisPort)
	if !conn.Ping(redisServer, config.RedisPassword) {
		log.Fatal("connect to redis server failed")
	}
	conn.Pool = conn.NewPool(redisServer, config.RedisPassword, config.RedisDB)

	//create robot and run
	robot := newRobot(config.RobotToken, config.RobotName, config.WebHookUrl)
	robot.bot.Debug = debug
	go robot.run()

	//run server and web samaritan
	srvPort := strconv.Itoa(config.Port)
	http.HandleFunc("/ajax", ajax)
	http.HandleFunc("/websocket", socketHandler)
	http.HandleFunc("/talk", groupTalk)
	log.Fatal(http.ListenAndServeTLS(net.JoinHostPort(config.Server, srvPort), config.Cert, config.CertKey, nil))

}

func groupTalk(w http.ResponseWriter, r *http.Request) {
	tlChan := make(chan string, 5)
	qinChan := make(chan string, 5)
	iceChan := make(chan string, 5)
	initSentence := "你好"
	tlChan <- qinAI(initSentence)
	go func() {
		for {
			msgToTl := <-tlChan
			replyFromTl := tlAI(msgToTl)
			qinChan <- replyFromTl
			//iceChan <- replyFromTl
		}
	}()

	go func() {
		for {
			msgToIce := <-iceChan
			replyFromIce := iceAI(msgToIce)
			tlChan <- replyFromIce
			qinChan <- replyFromIce
		}
	}()

	for {
		msgToQin := <-qinChan
		replyFromQin := qinAI(msgToQin)
		iceChan <- replyFromQin
		tlChan <- replyFromQin
	}

	//for {
	//	select {
	//	case msgToIce := <-iceChan:
	//		replyFromIce := iceAI(msgToIce)
	//		tlChan <- replyFromIce
	//		qinChan <- replyFromIce
	//	case msgToTl := <-tlChan:
	//		time.Sleep(time.Second)
	//		replyFromTl := tlAI(msgToTl)
	//		qinChan <- replyFromTl
	//		iceChan <- replyFromTl
	//	case msgToQin := <-qinChan:
	//		time.Sleep(time.Second)
	//		replyFromQin := qinAI(msgToQin)
	//		iceChan <- replyFromQin
	//		tlChan <- replyFromQin

}

//used for web samaritan robot
func socketHandler(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()
	for {
		var in []byte
		var ret []string
		mt, in, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		ret = receive(string(in))
		for i := range ret {
			c.WriteMessage(mt, []byte(ret[i]))
			time.Sleep(time.Second)
		}
		c.WriteMessage(mt, []byte(""))
	}
}

//when webSocket unavailable, fallback to ajax long polling
func ajax(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Access-Control-Allow-Origin", "*")
	var messages = make(chan string)
	if r.Method == "GET" {
		w.Write([]byte(<-messages))
	} else {
		body := r.FormValue("text")
		if body != "" {
			go func(string) {
				ret := receive(body)
				for i := range ret {
					messages <- ret[i]
					time.Sleep(time.Second)
				}
				messages <- ""
			}(body)
		}
	}
}

//receive from client
func receive(in string) (ret []string) {
	defer func() {
		if p := recover(); p != nil {
			err := fmt.Errorf("client closed error: %v", p)
			log.Println(err)
		}
	}()
	fmt.Printf("Received: %s\n", in)
	var response string
	var answer = make(chan string)
	sf := func(c rune) bool {
		return c == ',' || c == '，' || c == ';' || c == '。' || c == '.' || c == '？' || c == '?'
	}
	if chinese(in) {
		go func() {
			answer <- iceAI(in)
		}()
		go func() {
			answer <- tlAI(in)
		}()
		go func() {
			ret := qinAI(in)
			if ret != "" {
				answer <- strings.Replace(ret, "Jarvis", "samaritan", -1)
			}
		}()
		response = <-answer
		// Separate into fields with func.
		ret = strings.FieldsFunc(response, sf)

	} else {
		response = mitAI(in)
		ret = strings.FieldsFunc(response, sf)
	}
	return
}
