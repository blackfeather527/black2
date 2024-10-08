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
	domains, uniqueDomains := &sync.Map{}, make(map[string]struct{}, 1000)
	file, err := os.Open(inputFile)
	if err != nil {
		log.Printf("无法打开输入文件: %v", err)
		return domains
	}
	defer file.Close()

	lineCount, validDomainCount := 0, 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		lineCount++
		if u, err := url.Parse(strings.TrimSpace(scanner.Text())); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			host, port, isDefaultPort := u.Hostname(), u.Port(), (u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443")
			dedupeKey := host + (map[bool]string{true: "", false: ":" + port}[isDefaultPort || port == ""])
			if _, exists := uniqueDomains[dedupeKey]; !exists {
				uniqueDomains[dedupeKey], validDomainCount = struct{}{}, validDomainCount+1
				formattedURL := u.Scheme + "://" + host + (map[bool]string{true: "", false: ":" + port}[isDefaultPort || port == ""])
				domains.Store(formattedURL, struct{}{})
				log.Printf("组合域名: %s", formattedURL)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("读取文件时出错: %v", err)
		return &sync.Map{}
	}
	log.Printf("总行数: %d, 有效域名数: %d", lineCount, validDomainCount)
	return domains
}
