package main

import (
    "bufio"
    "flag"
    "fmt"
    "log"
    "net/url"
    "os"
    "strings"
)

func main() {
    // 定义命令行参数
    inputPath := flag.String("input", "domains.txt", "输入文件路径")
    flag.StringVar(inputPath, "i", "domains.txt", "输入文件路径（短形式）")
    
    outputDir := flag.String("output", ".", "输出文件目录")
    flag.StringVar(outputDir, "o", ".", "输出文件目录（短形式）")

    // 设置 Usage 函数来自定义帮助信息
    flag.Usage = func() {
        fmt.Fprintf(os.Stderr, "用法: %s [选项]\n", os.Args[0])
        fmt.Fprintf(os.Stderr, "选项:\n")
        flag.PrintDefaults()
    }

    // 解析命令行参数
    flag.Parse()

    // 忽略未定义的参数
    flag.CommandLine.Init(os.Args[0], flag.ContinueOnError)
    args := os.Args[1:]
    for len(args) > 0 {
        if err := flag.CommandLine.Parse(args); err != nil {
            args = args[1:]
        } else {
            break
        }
    }

    // 打印所有命令行参数，以防出现未使用警告
    log.Printf("输入文件路径: %s", *inputPath)
    log.Printf("输出文件目录: %s", *outputDir)

    // 调用函数处理输入文件
    validDomains := processInputFile(*inputPath)

    // 在这里可以继续处理 validDomains
    log.Printf("有效域名总数: %d", len(validDomains))
}

func processInputFile(inputPath string) []string {
    file, err := os.Open(inputPath)
    if err != nil {
        log.Fatalf("打开输入文件时出错: %v", err)
    }
    defer file.Close()

    scanner := bufio.NewScanner(file)
    var validDomains []string

    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" {
            continue // 跳过空行
        }

        // 解析并验证 URL
        u, err := url.Parse(line)
        if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
            log.Printf("无效的 URL: %s", line)
            continue
        }

        // 构建标准化的 URL
        standardURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
        if u.Port() != "" {
            standardURL += ":" + u.Port()
        }

        validDomains = append(validDomains, standardURL)
        log.Printf("有效域名: %s", standardURL)
    }

    if err := scanner.Err(); err != nil {
        log.Fatalf("读取输入文件时出错: %v", err)
    }

    log.Printf("处理完成。共找到 %d 个有效域名", len(validDomains))
    return validDomains
}
