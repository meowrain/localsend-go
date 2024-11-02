package config

import (
	"embed"
	"log"
	"os"

	"gopkg.in/yaml.v2"
)

//go:embed config.yaml
var embeddedConfig embed.FS

type Config struct {
	NameOfDevice string `yaml:"name"`
	Functions    struct {
		HttpFileServer  bool `yaml:"http_file_server"`
		LocalSendServer bool `yaml:"local_send_server"`
	} `yaml:"functions"`
}

var ConfigData Config

func init() {
	bytes, err := os.ReadFile("internal/config/config.yaml")
	if err != nil {
		log.Println("读取外部配置文件失败，使用内置配置")
		bytes, err = embeddedConfig.ReadFile("config.yaml")
		if err != nil {
			log.Fatalf("无法读取嵌入式配置文件: %v", err)
		}
	}

	if err := yaml.Unmarshal(bytes, &ConfigData); err != nil {
		log.Fatalf("解析配置文件出错: %v", err)
	}
}
