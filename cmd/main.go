package main

import (
	"flag"
	"fmt"
	"localsend_cli/internal/discovery"
	"localsend_cli/internal/handlers"
	"localsend_cli/internal/pkg/server"
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
	httpServer.HandleFunc("/upload", handlers.SendHandler)      // 上传处理程序
	httpServer.HandleFunc("/download", handlers.ReceiveHandler) // 下载处理程序
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
		if err := sendFile(*filePath); err != nil {
			log.Fatalf("Send failed: %v", err)
		}
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
