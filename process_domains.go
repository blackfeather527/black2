package main

import (
    "bufio"
    "context"
    "crypto/tls"
    "flag"
    "io/ioutil"
    "log"
    "net"
    "net/http"
    "net/url"
    "os"
    "strings"
    "sync"
    "sync/atomic"
    "time"

    "gopkg.in/yaml.v2"
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

	    proxiesMap := fetchAndParseProxies(validDomains)

    // 输出代理数量
    proxyCount := 0
    proxiesMap.Range(func(key, value interface{}) bool {
        proxyCount++
        return true
    })
    log.Printf("成功解析的代理数量: %d", proxyCount)

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
        concurrency, tcpTimeout, tcpRetries, reportInterval = 50, 5 * time.Second, 3, 5 * time.Second
    )
    validDomains, total, success := &sync.Map{}, int64(0), int64(0)
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    go func() {
        for range time.Tick(reportInterval) {
            select {
            case <-ctx.Done(): return
            default: log.Printf("进度：总数 %d，成功 %d", atomic.LoadInt64(&total), atomic.LoadInt64(&success))
            }
        }
    }()

    var wg sync.WaitGroup
    sem, dialer := make(chan struct{}, concurrency), &net.Dialer{Timeout: tcpTimeout}

    domains.Range(func(key, _ interface{}) bool {
        wg.Add(1)
        go func(domain string) {
            defer wg.Done(); sem <- struct{}{}; defer func() { <-sem }()
            atomic.AddInt64(&total, 1)
            u, err := url.Parse(domain)
            if err != nil { return }
            host, port := u.Hostname(), u.Port()
            if port == "" { port = map[string]string{"https": "443", "http": "80"}[u.Scheme] }
            for i := 0; i < tcpRetries; i++ {
                if conn, err := dialer.Dial("tcp", net.JoinHostPort(host, port)); err == nil {
                    conn.Close(); validDomains.Store(domain, struct{}{}); atomic.AddInt64(&success, 1); return
                }
                time.Sleep(time.Second)
            }
        }(key.(string))
        return true
    })

    wg.Wait(); cancel()
    log.Printf("最终结果：总数 %d，成功 %d", atomic.LoadInt64(&total), atomic.LoadInt64(&success))
    return validDomains
}

func fetchAndParseProxies(validDomains *sync.Map) *sync.Map {
    const (
        concurrency = 50
        timeout     = 10 * time.Second
        proxyPath   = "/clash/proxies"
    )

    proxiesMap := &sync.Map{}
    totalProxies := int64(0)
    var wg sync.WaitGroup
    sem := make(chan struct{}, concurrency)

    client := &http.Client{
        Timeout: timeout,
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
        },
    }

    validDomains.Range(func(key, _ interface{}) bool {
        wg.Add(1)
        go func(domain string) {
            defer wg.Done()
            sem <- struct{}{}
            defer func() { <-sem }()

            url := domain + proxyPath
            resp, err := client.Get(url)
            if err != nil {
                log.Printf("获取 %s 失败: %v", url, err)
                return
            }
            defer resp.Body.Close()

            if resp.StatusCode != http.StatusOK {
                log.Printf("%s 返回非200状态码: %d", url, resp.StatusCode)
                return
            }

            body, err := ioutil.ReadAll(resp.Body)
            if err != nil {
                log.Printf("读取 %s 的响应体失败: %v", url, err)
                return
            }

            var config map[string]interface{}
            err = yaml.Unmarshal(body, &config)
            if err != nil {
                log.Printf("解析 %s 的YAML失败: %v", url, err)
                return
            }

            proxies, ok := config["proxies"].([]interface{})
            if !ok {
                log.Printf("%s 中没有找到有效的proxies段", url)
                return
            }

            log.Printf("从 %s 获取到配置文件", url)
            for i, proxy := range proxies {
                if proxyStr, ok := proxy.(string); ok {
                    proxiesMap.Store(domain+"|"+proxyStr, struct{}{})
                    atomic.AddInt64(&totalProxies, 1)

                    if i < 3 {
                        log.Printf("示例代理 %d: %s", i+1, proxyStr)
                    }
                }
                if i == 2 {
                    break // 只显示前三个
                }
            }
            log.Printf("从 %s 总共解析到 %d 个代理", url, len(proxies))
        }(key.(string))
        return true
    })

    wg.Wait()

    log.Printf("总共获取到 %d 个代理信息", atomic.LoadInt64(&totalProxies))
    return proxiesMap
}
