package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"localsend_cli/internal/config"
	"localsend_cli/internal/discovery"
	"localsend_cli/internal/handlers"
	"localsend_cli/internal/pkg/server"
	"localsend_cli/internal/utils/logger"
	"localsend_cli/static"
)

func main() {
	logger.InitLogger()
	mode := flag.String("mode", "send", "Mode of operation: send or receive")
	filePath := flag.String("file", "", "Path to the file to upload")
	toDevice := flag.String("to", "", "Send file to Device ip,Write device receiver ip here")
	flag.Parse()

	// Start broadcast and listening functionality
	go discovery.ListenForBroadcasts()
	go discovery.StartBroadcast()
	go discovery.StartHTTPBroadcast() // Start HTTP broadcast

	// Start HTTP server
	httpServer := server.New()
	if config.ConfigData.Functions.HttpFileServer {

		// If HTTP file server is enabled, enable the following routes
		httpServer.HandleFunc("/", handlers.IndexFileHandler)
		httpServer.HandleFunc("/uploads/", handlers.FileServerHandler)
		httpServer.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static.EmbeddedStaticFiles))))
		httpServer.HandleFunc("/send", handlers.NormalSendHandler)       // Upload handler
		httpServer.HandleFunc("/receive", handlers.NormalReceiveHandler) // Download handler
	}
	/* Send and receive section */
	if config.ConfigData.Functions.LocalSendServer {
		httpServer.HandleFunc("/api/localsend/v2/prepare-upload", handlers.PrepareReceive)
		httpServer.HandleFunc("/api/localsend/v2/upload", handlers.ReceiveHandler)
		httpServer.HandleFunc("/api/localsend/v2/info", handlers.GetInfoHandler)
	}
	go func() {
		logger.Info("Server started at :53317")
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
			logger.Errorf("Send failed: %v", err)
		}

	case "receive":
		logger.Info("Waiting to receive files...")
		ips, _ := discovery.GetLocalIP()
		local_ips := make([]string, 0)

		for _, ip := range ips {
			if strings.HasPrefix(ip.String(), "192.168") {
				local_ips = append(local_ips, ip.String())
			}
		}

		logger.Infof("If you opened http file server,you can view your files on %s", fmt.Sprintf("http://%v:53317", local_ips[0]))
		select {} // Block the program to wait for receiving files
	default:
		flag.Usage()
		os.Exit(1)
	}
}
