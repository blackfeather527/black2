package main

import (
    
    "encoding/base64"
    "encoding/json"
    "io"
    "io/ioutil"
    "net/http"
    "strings"
    "sync"
    "sync/atomic"
    "time"
    "bufio"
    "context"
    "crypto/tls"
    "database/sql"
    "flag"
    "fmt"
    "net/url"
    "os"
    "unicode/utf8"

    "golang.org/x/time/rate"
    _ "github.com/mattn/go-sqlite3"
)

const (
    maxConcurrent = 10
    maxRetries    = 3
    timeout       = 5 * time.Second
)

var (
    inputPath        *string
    outputDir        *string
    failureThreshold *int
    silentDays       *int
    dbPath           *string
)

var limiter = rate.NewLimiter(rate.Every(time.Second/10), maxConcurrent)

type Domain struct {
    Protocol string
    Host     string
    Port     string
}

type ProxyInfo struct {
    Protocol string
    FullInfo string
}

type VmessInfo struct {
    Add  string `json:"add"`
    Port int    `json:"port"`
    ID   string `json:"id"`
    Aid  string `json:"aid"`
    Ps   string `json:"ps"`
    V    string `json:"v"`
    Net  string `json:"net"`
    Type string `json:"type"`
    Host string `json:"host"`
    Path string `json:"path"`
    TLS  string `json:"tls"`
}

func init() {
    // 定义命令行参数
    inputPath = flag.String("input", "domains.txt", "输入文件路径")
    outputDir = flag.String("output", ".", "输出文件目录")
    failureThreshold = flag.Int("failures", 5, "检测失败次数阈值")
    silentDays = flag.Int("silent", 7, "检测静默天数")
    dbPath = flag.String("db", "domains.db", "SQLite 数据库路径")
}

func main() {
    // 解析命令行参数
    flag.Parse()

    // 打印命令行参数以防止未使用警告
    fmt.Printf("输入文件路径: %s\n", *inputPath)
    fmt.Printf("输出文件目录: %s\n", *outputDir)
    fmt.Printf("检测失败次数阈值: %d\n", *failureThreshold)
    fmt.Printf("检测静默天数: %d\n", *silentDays)
    fmt.Printf("SQLite 数据库路径: %s\n", *dbPath)

    // 调用函数处理输入文件
    domains := processDomainFile(*inputPath)

    // 检查域名的有效性
    validDomains := checkDomains(domains)
    
    allProxies := fetchProxies(validDomains, "vmess/sub")
}

// processDomainFile 函数用于处理包含域名的文件
// 输入参数 inputPath 是文件的路径
// 返回一个 Domain 结构体的切片，包含所有解析成功的唯一域名
func processDomainFile(inputPath string) []Domain {
    // 打开文件
    file, err := os.Open(inputPath)
    if err != nil {
        fmt.Printf("打开文件失败: %v\n", err)
        return nil
    }
    defer file.Close() // 确保在函数结束时关闭文件

    // 初始化一个map来存储唯一的域名
    // 预分配容量为1000，减少后续可能的扩容操作
    domainMap := make(map[string]Domain, 1000)
    scanner := bufio.NewScanner(file) // 创建一个scanner来读取文件
    lineCount := 0  // 记录总行数
    validCount := 0 // 记录有效域名数

    // 创建一个strings.Builder用于高效地构建字符串
    var keyBuilder strings.Builder
    keyBuilder.Grow(256) // 预分配Builder的容量，减少内存分配

    // 逐行读取文件
    for scanner.Scan() {
        lineCount++
        line := strings.TrimSpace(scanner.Text()) // 去除行首尾的空白字符
        if line == "" {
            continue // 跳过空行
        }

        // 解析URL
        u, err := url.Parse(line)
        if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
            fmt.Printf("第 %d 行格式不正确: %s\n", lineCount, line)
            continue
        }

        // 创建Domain结构体
        domain := Domain{
            Protocol: u.Scheme,
            Host:     u.Hostname(),
            Port:     u.Port(),
        }

        // 如果端口为空，根据协议设置默认端口
        if domain.Port == "" {
            if domain.Protocol == "http" {
                domain.Port = "80"
            } else {
                domain.Port = "443"
            }
        }

        // 使用strings.Builder构建map的key
        keyBuilder.Reset() // 重置Builder
        keyBuilder.WriteString(domain.Host)
        if domain.Port != "80" && domain.Port != "443" {
            keyBuilder.WriteByte(':')
            keyBuilder.WriteString(domain.Port)
        }
        key := keyBuilder.String()
        
        // 如果域名不存在于map中，则添加
        if _, exists := domainMap[key]; !exists {
            domainMap[key] = domain
            validCount++
        }
    }

    // 检查是否在读取文件时发生错误
    if err := scanner.Err(); err != nil {
        fmt.Printf("读取文件时发生错误: %v\n", err)
    }

    // 将map中的域名转换为切片
    domains := make([]Domain, 0, validCount) // 预分配切片容量
    for _, domain := range domainMap {
        domains = append(domains, domain)
    }

    // 打印统计信息
    fmt.Printf("总行数: %d, 唯一有效域名数: %d\n", lineCount, validCount)
    return domains
}

func checkDomains(domains []Domain) []Domain {
    db, err := initDB()
    if err != nil {
        fmt.Printf("初始化数据库失败: %v\n", err)
        return nil
    }
    defer db.Close()

    var validDomains []Domain
    var wg sync.WaitGroup
    var mu sync.Mutex
    semaphore := make(chan struct{}, maxConcurrent)

    for _, domain := range domains {
        wg.Add(1)
        go func(d Domain) {
            defer wg.Done()
            semaphore <- struct{}{}
            defer func() { <-semaphore }()

            if err := limiter.Wait(context.Background()); err != nil {
                fmt.Printf("限速器错误: %v\n", err)
                return
            }

            shouldCheck, _ := shouldCheckDomain(db, d)
            if !shouldCheck {
                return
            }

            if isValidDomain(d) {
                mu.Lock()
                validDomains = append(validDomains, d)
                mu.Unlock()
                fmt.Printf("有效域名: %s://%s:%s\n", d.Protocol, d.Host, d.Port)
                updateDomainStatus(db, d, true)
            } else {
                updateDomainStatus(db, d, false)
            }
        }(domain)
    }

    wg.Wait()

    fmt.Printf("检查完成，有效域名数量: %d\n", len(validDomains))
    return validDomains
}

func initDB() (*sql.DB, error) {
    db, err := sql.Open("sqlite3", *dbPath)
    if err != nil {
        return nil, err
    }

    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS domain_checks (
            domain TEXT PRIMARY KEY,
            failure_count INT,
            last_check DATETIME
        )
    `)
    if err != nil {
        return nil, err
    }

    return db, nil
}

func shouldCheckDomain(db *sql.DB, domain Domain) (bool, int) {
    var failureCount int
    var lastCheck time.Time
    err := db.QueryRow("SELECT failure_count, last_check FROM domain_checks WHERE domain = ?", 
                       fmt.Sprintf("%s://%s:%s", domain.Protocol, domain.Host, domain.Port)).
           Scan(&failureCount, &lastCheck)

    if err == sql.ErrNoRows {
        return true, 0
    } else if err != nil {
        fmt.Printf("查询域名状态失败: %v\n", err)
        return true, 0
    }

    if failureCount >= *failureThreshold && time.Since(lastCheck) < time.Duration(*silentDays)*24*time.Hour {
        return false, failureCount
    }

    return true, failureCount
}

func updateDomainStatus(db *sql.DB, domain Domain, isValid bool) {
    domainStr := fmt.Sprintf("%s://%s:%s", domain.Protocol, domain.Host, domain.Port)
    if isValid {
        _, err := db.Exec("DELETE FROM domain_checks WHERE domain = ?", domainStr)
        if err != nil {
            fmt.Printf("删除有效域名记录失败: %v\n", err)
        }
    } else {
        _, err := db.Exec(`
            INSERT INTO domain_checks (domain, failure_count, last_check)
            VALUES (?, 1, CURRENT_TIMESTAMP)
            ON CONFLICT(domain) DO UPDATE SET
            failure_count = failure_count + 1,
            last_check = CURRENT_TIMESTAMP
        `, domainStr)
        if err != nil {
            fmt.Printf("更新域名状态失败: %v\n", err)
        }
    }
}

func isValidDomain(domain Domain) bool {
    url := fmt.Sprintf("%s://%s:%s", domain.Protocol, domain.Host, domain.Port)
    client := &http.Client{
        Timeout: timeout,
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
        },
    }

    for i := 0; i < maxRetries; i++ {
        resp, err := client.Get(url)
        if err != nil {
            time.Sleep(time.Second * time.Duration(i+1))
            continue
        }
        defer resp.Body.Close()

        body, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            fmt.Printf("读取 %s 的响应体时出错: %v\n", url, err)
            continue
        }

        // 将 body 转换为 UTF-8 编码
        bodyUTF8 := make([]rune, 0, len(body))
        for len(body) > 0 {
            r, size := utf8.DecodeRune(body)
            if r == utf8.RuneError {
                body = body[1:]
            } else {
                bodyUTF8 = append(bodyUTF8, r)
                body = body[size:]
            }
        }
        bodyString := string(bodyUTF8)

        // 检查响应体是否包含特定字符串
        if strings.Contains(bodyString, "Sansui233") && strings.Contains(bodyString, "目前共有抓取源") {
            return true
        }
        break
    }
    return false
}


// fetchProxies 函数用于从给定的域名列表中获取代理订阅信息
//
// 主要功能：
// 1. 并发地从多个域名获取订阅信息
// 2. 解码并解析订阅内容，提取有效的代理信息
// 3. 对所有获取到的代理信息进行去重
// 4. 控制并发数和请求速率，避免对服务器造成过大压力
//
// 参数：
// - domains: 包含多个Domain结构体的切片，每个结构体代表一个待查询的域名
// - relativePath: 获取订阅信息的相对路径
//
// 返回：
// - []string: 包含所有唯一的代理信息的字符串切片

func fetchProxies(domains []Domain, relativePath string) []string {
    // 确保相对路径以 "/" 开头
    if !strings.HasPrefix(relativePath, "/") {
        relativePath = "/" + relativePath
    }

    var allProxies []string  // 存储所有获取到的代理信息
    var mu sync.Mutex        // 互斥锁，用于保护 allProxies 的并发访问
    semaphore := make(chan struct{}, maxConcurrent)  // 信号量，用于限制并发数
    limiter := rate.NewLimiter(rate.Every(time.Second/10), maxConcurrent)  // 速率限制器

    // 配置 HTTP 客户端
    client := &http.Client{
        Timeout: timeout,
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: true},  // 忽略 SSL 证书验证
            MaxIdleConnsPerHost: maxConcurrent,  // 优化连接复用
        },
    }

    var wg sync.WaitGroup
    for _, domain := range domains {
        wg.Add(1)
        go func(d Domain) {
            defer wg.Done()
            semaphore <- struct{}{}  // 获取信号量
            defer func() { <-semaphore }()  // 释放信号量

            // 等待速率限制器的许可
            if err := limiter.Wait(context.Background()); err != nil {
                fmt.Printf("限速器错误: %v\n", err)
                return
            }

            // 构建完整的 URL
            url := fmt.Sprintf("%s://%s:%s%s", d.Protocol, d.Host, d.Port, relativePath)
            
            // 发送 GET 请求
            resp, err := client.Get(url)
            if err != nil {
                fmt.Printf("获取订阅失败 %s: %v\n", url, err)
                return
            }
            defer resp.Body.Close()

            // 读取响应体
            body, err := io.ReadAll(resp.Body)
            if err != nil {
                fmt.Printf("读取响应体失败 %s: %v\n", url, err)
                return
            }

            // Base64 解码
            decodedBody, err := base64.StdEncoding.DecodeString(string(body))
            if err != nil {
                fmt.Printf("Base64解码失败 %s: %v\n", url, err)
                return
            }

            // 分割解码后的内容为单独的代理信息
            proxies := strings.Split(strings.TrimSpace(string(decodedBody)), "\n")
            validProxies := make([]string, 0, len(proxies))

            // 验证并收集有效的代理信息
            for _, proxy := range proxies {
                if strings.Contains(proxy, "://") {
                    validProxies = append(validProxies, proxy)
                }
            }

            // 将有效代理添加到总列表中
            mu.Lock()
            allProxies = append(allProxies, validProxies...)
            mu.Unlock()

            fmt.Printf("从 %s 获取到 %d 条代理信息\n", url, len(validProxies))
        }(domain)
    }

    wg.Wait()  // 等待所有 goroutine 完成

    // 使用 map 进行去重
    uniqueProxies := make(map[string]struct{})
    for _, proxy := range allProxies {
        uniqueProxies[proxy] = struct{}{}
    }

    // 将去重后的代理信息转换为切片
    result := make([]string, 0, len(uniqueProxies))
    for proxy := range uniqueProxies {
        result = append(result, proxy)
    }

    fmt.Printf("总共获取到 %d 条代理信息，去重后剩余 %d 条\n", len(allProxies), len(result))
    return result
}
