package config

import (
	"embed"
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v2"
)

//go:embed config.yaml
var EmbeddedConfig embed.FS

type Config struct {
	NameOfDevice string `yaml:"name"`
	Functions    struct {
		HttpFileServer  bool `yaml:"http_file_server"`
		LocalSendServer bool `yaml:"local_send_server"`
	} `yaml:"functions"`
}

var ConfigData Config

func init() {
	var bytes []byte
	var err error

	// 尝试从外部文件系统读取配置文件
	bytes, err = os.ReadFile("internal/config/config.yaml")
	if err != nil {
		fmt.Println("读取外部配置文件失败，使用内置配置")
		// 如果读取外部文件失败，则从嵌入文件系统读取
		bytes, err = EmbeddedConfig.ReadFile("config.yaml")
		if err != nil {
			log.Fatalf("Error reading embedded config file: %v", err)
		}
	}

	err = yaml.Unmarshal(bytes, &ConfigData)
	if err != nil {
		log.Fatalf("Error parsing config file: %v", err)
	}
}
