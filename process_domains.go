package main

import (
    "bufio"
    "context"
    "io"
    "flag"
    "log"
    "net"
    "net/http"
    "net/url"
    "os"
    "strings"
    "sync"
    "sync/atomic"
    "time"

    "golang.org/x/text/encoding/simplifiedchinese"
    "golang.org/x/text/transform"
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

	// 读取域名
	domains := readDomains(*inputFile)
	
	// 检查域名
	validDomains := checkDomains(domains)

	// 统计有效域名数量
	validDomainCount := 0
	validDomains.Range(func(key, value interface{}) bool {
		validDomainCount++
		return true
	})
	log.Printf("检测通过的有效域名数: %d", validDomainCount)

	// 这里可以添加后续处理逻辑，例如将有效域名写入文件或数据库
	// 为了示例，我们只打印一些信息
	log.Printf("有效域名列表:")
	validDomains.Range(func(key, value interface{}) bool {
		log.Printf("- %s", key)
		return true
	})

	// 注意：outputDir, dbFile, errorThreshold, 和 refreshDays 
	// 在这个示例中没有被直接使用，但它们可能在后续的功能实现中用到

	log.Println("程序执行完毕")
}

// readDomains 函数从指定文件中读取域名，进行格式化和去重，并返回有效的域名列表
// 参数 inputFile 是输入文件的路径
// 返回值是一个 *sync.Map，其中包含所有有效且唯一的域名
func readDomains(inputFile string) *sync.Map {
	// 初始化 sync.Map 用于存储最终的域名列表，以及一个 map 用于去重
	// 预分配 uniqueDomains 的容量为 1000，以减少 map 扩容次数
	domains, uniqueDomains := &sync.Map{}, make(map[string]struct{}, 1000)

	// 打开输入文件
	file, err := os.Open(inputFile)
	if err != nil {
		log.Printf("无法打开输入文件: %v", err)
		return domains // 如果无法打开文件，返回空的 domains
	}
	defer file.Close() // 确保文件在函数结束时关闭

	lineCount, validDomainCount := 0, 0 // 初始化行数和有效域名计数器
	scanner := bufio.NewScanner(file)
	// 增加 scanner 的缓冲区大小到 1MB，提高读取效率
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	// 逐行读取文件内容
	for scanner.Scan() {
		lineCount++ // 增加行数计数
		// 解析每一行为 URL
		if u, err := url.Parse(strings.TrimSpace(scanner.Text())); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			// 提取主机名、端口，并判断是否为默认端口
			host, port, isDefaultPort := u.Hostname(), u.Port(), (u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443")
			// 构造用于去重的 key
			dedupeKey := host + (map[bool]string{true: "", false: ":" + port}[isDefaultPort || port == ""])
			// 检查是否为重复域名
			if _, exists := uniqueDomains[dedupeKey]; !exists {
				// 如果不是重复域名，添加到 uniqueDomains 并增加有效域名计数
				uniqueDomains[dedupeKey], validDomainCount = struct{}{}, validDomainCount+1
				// 构造格式化的 URL
				formattedURL := u.Scheme + "://" + host + (map[bool]string{true: "", false: ":" + port}[isDefaultPort || port == ""])
				// 将格式化的 URL 存储到 domains 中
				domains.Store(formattedURL, struct{}{})
			}
		}
	}

	// 检查是否在读取过程中发生错误
	if err := scanner.Err(); err != nil {
		log.Printf("读取文件时出错: %v", err)
		return &sync.Map{} // 如果发生错误，返回空的 sync.Map
	}

	// 输出统计信息
	log.Printf("总行数: %d, 有效域名数: %d", lineCount, validDomainCount)
	return domains // 返回处理后的域名列表
}


func checkDomains(domains *sync.Map) *sync.Map {
    const (
        concurrency      = 50
        tcpTimeout       = 5 * time.Second
        httpTimeout      = 5 * time.Second
        tcpRetries       = 3
        httpRetries      = 3
        progressInterval = 5 * time.Second
        bufferSize       = 4096
    )

    validDomains, wg := &sync.Map{}, sync.WaitGroup{}
    var totalCount, tcpSuccessCount, validCount int64
    semaphore := make(chan struct{}, concurrency)
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    go func() {
        for range time.Tick(progressInterval) {
            select {
            case <-ctx.Done(): return
            default: log.Printf("进度: 总数 %d, TCP 成功 %d, 有效 %d", atomic.LoadInt64(&totalCount), atomic.LoadInt64(&tcpSuccessCount), atomic.LoadInt64(&validCount))
            }
        }
    }()
    dialer, client := &net.Dialer{Timeout: tcpTimeout}, &http.Client{
        Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: tcpTimeout}).DialContext, MaxIdleConns: 100, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ExpectContinueTimeout: 1 * time.Second},
        Timeout: httpTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
    }

    domains.Range(func(key, value interface{}) bool {
        wg.Add(1)
        go func(domain string) {
            defer wg.Done()
            semaphore <- struct{}{}
            defer func() { <-semaphore }()
            atomic.AddInt64(&totalCount, 1)

            u, _ := url.Parse(domain)
            host, port := u.Hostname(), u.Port()
            if port == "" { port = map[string]string{"https": "443", "http": "80"}[u.Scheme] }

            for i := 0; i < tcpRetries && func() bool {
                conn, err := dialer.Dial("tcp", net.JoinHostPort(host, port))
                if err == nil { conn.Close(); atomic.AddInt64(&tcpSuccessCount, 1); return false }
                time.Sleep(time.Second)
                return i < tcpRetries-1
            }(); i++ {}

            var resp *http.Response
            for i := 0; i < httpRetries && func() bool {
                var err error
                if resp, err = client.Get(domain); err != nil {
                    time.Sleep(time.Second)
                    return i < httpRetries-1
                }
                return false
            }(); i++ {}
            if resp == nil { return }
            defer resp.Body.Close()

            var reader io.Reader = resp.Body
            if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "gbk") || strings.Contains(ct, "gb2312") {
                reader = transform.NewReader(resp.Body, simplifiedchinese.GBK.NewDecoder())
            }

            keywords, found := []string{"sansui233", "目前共有抓取源"}, [2]bool{}
            buffer, allFound := make([]byte, bufferSize), false
            for n, err := reader.Read(buffer); !allFound && err == nil; n, err = reader.Read(buffer) {
                content := strings.ToLower(string(buffer[:n]))
                for i, kw := range keywords {
                    found[i] = found[i] || strings.Contains(content, kw)
                }
                allFound = found[0] && found[1]
            }

            if allFound {
                validDomains.Store(domain, struct{}{})
                atomic.AddInt64(&validCount, 1)
            }
        }(key.(string))
        return true
    })

    wg.Wait()
    cancel()
    log.Printf("检测结果: 总数 %d, TCP 成功 %d, 有效 %d", atomic.LoadInt64(&totalCount), atomic.LoadInt64(&tcpSuccessCount), atomic.LoadInt64(&validCount))
    return validDomains
}
