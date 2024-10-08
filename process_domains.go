package main

import (
	"flag"
	"bufio"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
)

func main() {
	// 定义命令行参数
	inputFile := flag.String("i", "domains.txt", "输入文件路径")
	flag.StringVar(inputFile, "input", "domains.txt", "输入文件路径")

	outputDir := flag.String("o", ".", "输出文件目录")
	flag.StringVar(outputDir, "output", ".", "输出文件目录")

	dbFile := flag.String("d", "proxy.db", "SQLite数据库文件路径")
	flag.StringVar(dbFile, "database", "proxy.db", "SQLite数据库文件路径")

	errorThreshold := flag.Int("e", 3, "错误次数阈值")
	flag.IntVar(errorThreshold, "error-threshold", 3, "错误次数阈值")

	refreshDays := flag.Int("r", 7, "刷新天数")
	flag.IntVar(refreshDays, "refresh-days", 7, "刷新天数")

	// 解析命令行参数
	flag.Parse()

	// 打印所有输入的参数
	log.Printf("输入文件: %s", *inputFile)
	log.Printf("输出目录: %s", *outputDir)
	log.Printf("数据库文件: %s", *dbFile)
	log.Printf("错误次数阈值: %d", *errorThreshold)
	log.Printf("刷新天数: %d", *refreshDays)

	domains := readDomains(*inputFile)

	// 使用返回的 domains
	domainCount := 0
	domains.Range(func(key, value interface{}) bool {
		domainCount++
		return true
	})

	log.Printf("从 sync.Map 中读取到的总域名数: %d", domainCount)

	log.Println("程序执行完毕")
}

func readDomains(inputFile string) *sync.Map {
	domains := &sync.Map{}
	file, err := os.Open(inputFile)
	if err != nil {
		log.Fatalf("无法打开输入文件: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0
	validDomainCount := 0

	for scanner.Scan() {
		lineCount++
		if u, err := url.Parse(strings.TrimSpace(scanner.Text())); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			host := u.Host
			if u.Port() == "" {
				host = u.Hostname() + (map[string]string{"http": ":80", "https": ":443"}[u.Scheme])
			}
			formattedURL := u.Scheme + "://" + host
			domains.Store(formattedURL, struct{}{})
			log.Printf("组合域名: %s", formattedURL)
			validDomainCount++
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("读取文件时出错: %v", err)
	}

	if lineCount == 0 {
		log.Fatalf("输入文件为空")
	}

	log.Printf("总行数: %d, 有效域名数: %d", lineCount, validDomainCount)

	return domains
}
