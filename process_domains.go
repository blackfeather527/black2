package main

import (
    "bufio"
    "flag"
    "fmt"
    "net/url"
    "os"
    "strings"
)

// Domain 结构体用于存储解析后的域名信息
type Domain struct {
    Protocol string
    Host     string
    Port     string
}

func main() {
    // 定义命令行参数
    inputPath := flag.String("input", "domains.txt", "输入文件路径")
    outputDir := flag.String("output", ".", "输出文件目录")
    flag.Parse()

    // 打印命令行参数以防止未使用警告
    fmt.Printf("输入文件路径: %s\n", *inputPath)
    fmt.Printf("输出文件目录: %s\n", *outputDir)

    // 调用函数处理输入文件
    domains := processDomainFile(*inputPath)

    // 在这里可以继续处理 domains 列表
    fmt.Printf("总共处理了 %d 个唯一有效域名\n", len(domains))
}

// processDomainFile 函数用于处理输入文件并返回唯一有效的域名列表
func processDomainFile(inputPath string) []Domain {
    file, err := os.Open(inputPath)
    if err != nil {
        fmt.Printf("打开文件失败: %v\n", err)
        return nil
    }
    defer file.Close()

    domainMap := make(map[string]Domain)
    scanner := bufio.NewScanner(file)
    lineCount := 0
    validCount := 0

    for scanner.Scan() {
        lineCount++
        line := strings.TrimSpace(scanner.Text())
        if line == "" {
            continue
        }

        domain, ok := parseDomain(line)
        if !ok {
            fmt.Printf("第 %d 行格式不正确: %s\n", lineCount, line)
            continue
        }

        key := domain.Host + ":" + domain.Port
        if _, exists := domainMap[key]; !exists {
            domainMap[key] = domain
            validCount++
            fmt.Printf("有效域名: %s://%s:%s\n", domain.Protocol, domain.Host, domain.Port)
        }
    }

    if err := scanner.Err(); err != nil {
        fmt.Printf("读取文件时发生错误: %v\n", err)
    }

    domains := make([]Domain, 0, len(domainMap))
    for _, domain := range domainMap {
        domains = append(domains, domain)
    }

    fmt.Printf("总行数: %d, 唯一有效域名数: %d\n", lineCount, validCount)
    return domains
}

// parseDomain 函数用于解析单个域名行
func parseDomain(line string) (Domain, bool) {
    u, err := url.Parse(line)
    if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
        return Domain{}, false
    }

    domain := Domain{
        Protocol: u.Scheme,
        Host:     u.Hostname(),
        Port:     u.Port(),
    }

    if domain.Port == "" {
        if domain.Protocol == "http" {
            domain.Port = "80"
        } else {
            domain.Port = "443"
        }
    }

    return domain, true
}
