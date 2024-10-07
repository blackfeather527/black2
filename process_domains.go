package main

import (
    "bufio"
    "flag"
    "fmt"
    "net/url"
    "os"
    "strings"
)

// DomainInfo 结构体用于存储域名信息
type DomainInfo struct {
    Protocol string
    Domain   string
    Port     string
}

func main() {
    // 定义命令行参数
    inputPath := flag.String("input", "domains.txt", "输入文件路径")
    outputDir := flag.String("output", ".", "输出文件目录")
    flag.Parse()

    // 读取和解析输入文件
    domains, err := readAndParseDomains(*inputPath)
    if err != nil {
        fmt.Printf("读取和解析域名时出错: %v\n", err)
        return
    }

    // 这里可以继续处理 domains 列表
    // ...

    fmt.Printf("成功解析的域名总数: %d\n", len(domains))
}

// readAndParseDomains 读取输入文件并解析域名
func readAndParseDomains(inputPath string) ([]DomainInfo, error) {
    file, err := os.Open(inputPath)
    if err != nil {
        return nil, fmt.Errorf("打开输入文件时出错: %w", err)
    }
    defer file.Close()

    var domains []DomainInfo
    scanner := bufio.NewScanner(file)
    lineNumber := 0

    for scanner.Scan() {
        lineNumber++
        line := strings.TrimSpace(scanner.Text())

        if line == "" {
            continue // 跳过空行
        }

        domainInfo, err := parseDomainLine(line)
        if err != nil {
            fmt.Printf("行 %d: 解析错误 - %v\n", lineNumber, err)
            continue
        }

        domains = append(domains, domainInfo)
        fmt.Printf("有效域名: %s://%s%s\n", domainInfo.Protocol, domainInfo.Domain, domainInfo.Port)
    }

    if err := scanner.Err(); err != nil {
        return nil, fmt.Errorf("读取文件时出错: %w", err)
    }

    fmt.Printf("成功解析的域名总数: %d\n", len(domains))
    return domains, nil
}

// parseDomainLine 解析单行域名信息
func parseDomainLine(line string) (DomainInfo, error) {
    u, err := url.Parse(line)
    if err != nil {
        return DomainInfo{}, fmt.Errorf("无效的URL格式: %v", err)
    }

    if u.Scheme != "http" && u.Scheme != "https" {
        return DomainInfo{}, fmt.Errorf("无效的协议: %s", u.Scheme)
    }

    domainInfo := DomainInfo{
        Protocol: u.Scheme,
        Domain:   u.Hostname(),
        Port:     u.Port(),
    }

    if domainInfo.Port != "" {
        domainInfo.Port = ":" + domainInfo.Port
    }

    return domainInfo, nil
}
