package main

import (
	"flag"
	"fmt"
	"localsend_cli/internal/config"
	"localsend_cli/internal/discovery"
	"localsend_cli/internal/handlers"
	"localsend_cli/internal/pkg/server"
	"localsend_cli/static"
	"log"
	"net/http"
	"os"
)

func main() {
	mode := flag.String("mode", "send", "Mode of operation: send or receive")
	filePath := flag.String("file", "", "Path to the file to upload")
	toDevice := flag.String("to", "", "Send file to Device ip,Write device receiver ip here")
	flag.Parse()

	// // 启动广播和监听功能
	go discovery.ListenForBroadcasts()
	go discovery.StartBroadcast()
	go discovery.StartHTTPBroadcast() // 启动HTTP广播

	// 启动HTTP服务器
	httpServer := server.New()
	if config.ConfigData.Functions.HttpFileServer {

		//如果启用http文件服务器，启用下面的路由
		httpServer.HandleFunc("/", handlers.IndexFileHandler)
		httpServer.HandleFunc("/uploads/", handlers.FileServerHandler)
		httpServer.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static.EmbeddedStaticFiles))))
	}
	/*发送接收部分*/
	if config.ConfigData.Functions.LocalSendServer {

		httpServer.HandleFunc("/api/localsend/v2/prepare-upload", handlers.PrepareReceive)
		httpServer.HandleFunc("/api/localsend/v2/upload", handlers.ReceiveHandler)
		httpServer.HandleFunc("/api/localsend/v2/info", handlers.GetInfoHandler)
		httpServer.HandleFunc("/send", handlers.NormalSendHandler)       // 上传处理程序
		httpServer.HandleFunc("/receive", handlers.NormalReceiveHandler) // 下载处理程序

	}
	go func() {
		log.Println("Server started at :53317")
		if err := http.ListenAndServe(":53317", httpServer); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	switch *mode {
	case "send":
		if *filePath == "" {
			fmt.Println("Send mode requires a file path")
			flag.Usage()
			os.Exit(1)
		}
		if *toDevice == "" {
			fmt.Println("Send mode requires a toDevice")
			flag.Usage()
			os.Exit(1)
		}
		err := handlers.SendFile(*toDevice, *filePath)
		if err != nil {
			log.Fatalf("Send failed: %v", err)
		}
		// if err := sendFile(*filePath); err != nil {
		// 	log.Fatalf("Send failed: %v", err)
		// }
	case "receive":
		fmt.Println("Waiting to receive files...")
		select {} // 阻塞程序等待接收文件
	default:
		flag.Usage()
		os.Exit(1)
	}
}

func sendFile(filePath string) error {
	fmt.Println("Sending file:", filePath)
	// 上传文件的逻辑
	return nil
}
