package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type SystemInfo struct {
	Hostname   string		`json:"hostname"`
	IP         string		`json:"ip_address"`
	OSType     string		`json:"os_type"`
	OSVersion  string		`json:"os_version"`
	Kernel	   string		`json:"kernel"`
	Uptime     string		`json:"uptime"`
	LoadingAvg string		`json:"loading_avg"`
	CPU        string		`json:"cpu_usage"`
	Memory     string		`json:"memory_usage"`
	Disk       string		`json:"disk_usage"`
	Timestamp  int64		`json:"timestamp"`
	Services   map[string]string    `jsonL"services"`
}

// Конфигурация
type Config struct {
	ServerURL  string    `json:"server_url"`
	Interval   int       `json:"interval_seconds"`
	Services   []string  `json:"services"`
}

func main() {
	// Настройки логов
	logFile, err := os.OpenFile("/opt/astra-agent/agent.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal("не могу открыть лог-файл:", err)
	}
	defer logFile.Close()
	log.SetOutput(io.MultiWriter(logFile, os.Stdout))

	log.Println("//\\ Агент запущен //\\")

	// Загрузка конфига
	config := loadConfig("./config.json")
	log.Printf("Сервер: %s, итнервал: %d сек", config.ServerURL, config.Interval)

	log.Println("ПРИНУДИТЕЛЬНАЯ ПРОВЕРКА КОМАНД ПРИ ЗАПУСКЕ")
	checkAndExecuteCommands(config.ServerURL)
	log.Println("ПРИНУДИТЕЛЬНАЯ ПРОВЕРКА КОМАНД ПРИ ЗАПУСКЕ")

	// Отправка сразу при старте
	sendSystemInfo(config.ServerURL)

	// Бесконечный цикл
	interval := time.Duration(config.Interval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	commandTicker := time.NewTicker(10 * time.Second)
	defer commandTicker.Stop()



	//for range ticker.C {
	//	sendSystemInfo(config.ServerURL)
	//}

	// commands parth start


	log.Println(" Запуск основного цикла...")



	for {
		select {
		case <-ticker.C:
			sendSystemInfo(config.ServerURL)
		case <-commandTicker.C:
			log.Println("Проверка команд по таймеру...")
			checkAndExecuteCommands(config.ServerURL)
		}
	}



	// commands parth end
}

// Основная функция сбора и отправки
func sendSystemInfo(ServerURL string) {
	info := collectSystemInfo()

	jsonData, err := json.Marshal(info)
	if err != nil {
		log.Println("Ошибка маршалинга JSON:", err)
		return
	}

	// отправка с таймаутом
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(ServerURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("!! Ошибка отправки на %s: %v !!", ServerURL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		log.Printf("//\\ Успешно отправлены данные с %s //\\", info.Hostname)
	} else {
		log.Printf("!!  Сервер ответил кодом: %d !!", resp.StatusCode)
	}
}

// Сбор системной информации
func collectSystemInfo() SystemInfo {
	hostname, _ := os.Hostname()
	ip := getIP()

	version := readFile("/etc/astra_version")
	if version == "" {
		version = readFile("/etc/os-release")
	}
	config := loadConfig("./config.json")

	return SystemInfo{
		Hostname:   hostname,
		IP:         ip,
		OSType:     runtime.GOOS,
		OSVersion:  getAstraVersion(),
		Kernel:     cleanString(readFile("/proc/sys/kernel/osrelease")),
		Uptime:     runCommand("uptime", "-p"),
		LoadingAvg: runCommand("uptime", "|", "awk", "-F", "'load average:'", "'{print $2}'"),
		CPU:        getCPUUsage(),
		Memory:     getMemoryUsage(),
		Disk:       getDiskUsage(),
		Timestamp:  time.Now().Unix(),
		Services:   getServicesStatus(config.Services),
	}
}

// Вспомогательные функции
func getIP() string {
	cmd := exec.Command("hostname", "-I")
	out, err := cmd.Output()
	if err != nil {
		return "unknow"
	}
	ips := strings.Fields(string(out))
	if len(ips) > 0 {
		return ips[0]
	}
	return "unknow"
}

func getCPUUsage() string {
	cmd := exec.Command("top", "-bn2", "-d", "0.5")
	out, err := cmd.Output()
	if err != nil {
		return "0%"
	}

	lines := strings.Split(string(out), "\n")

	for _, line := range lines {
		if strings.Contains(line, "%Cpu(s):") || strings.Contains(line, "Cpu") {
			fields := strings.Fields(line)

			var user, system, idle, nice float64
			for i, field := range fields {
				if field == "us," || field == "us" {
					if i+1 < len(fields) {
						fmt.Sscanf(fields[i+1], "%f", &user)
					}
				}
				if field == "sy," || field == "sy" {
					if i+1 < len(fields) {
						fmt.Sscanf(fields[i+1], "%f", &system)
					}
				}
				if field == "ni," || field == "ni" {
					if i+1 < len(fields) {
						fmt.Sscanf(fields[i+1], "%f", &nice)
					}
				}
				if field == "id," || field == "id" {
					if i+1 < len(fields) {
						fmt.Sscanf(fields[i+1], "%f", &idle)
					}
				}
			}

			if user == 0 && system == 0 && idle == 0 {
				parts := strings.Split(line, ":")
				if len(parts) > 1 {
					values := strings.Split(parts[1], ",")
					if len(values) >= 4 {
						fmt.Sscanf(strings.TrimSpace(values[0]), "%f", &user)
						fmt.Sscanf(strings.TrimSpace(values[1]), "%f", &system)
						fmt.Sscanf(strings.TrimSpace(values[2]), "%f", &nice)
						fmt.Sscanf(strings.TrimSpace(values[3]), "%f", &idle)
					}
				}
			}

			log.Printf("parts - %s", user)
			log.Printf("fields - %s", fields)

			total := user + system + nice + idle
			if total > 0 {
				//usage := ((user + system + nice) / total) * 100
				usage := user
				log.Printf("usage - %s", usage)
				return fmt.Sprintf("%.1f%%", usage)
			}
			return "0%"
		}
	}
	return getCPUUsage_proc()
}

func getCPUUsage_proc() string {
	// читаем /proc/stat
	log.Printf("getCPUUsage_proc")

	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return "0%"
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}

		var user, nice, system, idle, iowait, irq, softirq, steal int64
		fmt.Sscanf(line, "cpu %d %d %d %d %d %d %d %d ", &user, &nice, &system, &idle, &iowait, &irq, &softirq, &steal)
		total := user + nice + system + idle + iowait + irq + softirq + steal
		idleAll := idle + iowait

		if total == 0 {
			return "0%"
		}

		time.Sleep(500 * time.Millisecond)



		data2, _ := os.ReadFile("/proc/stat")
		lines2 := strings.Split(string(data2), "\n")
		for _, line2 := range lines2 {
			if !strings.HasPrefix(line, "cpu ") {
				continue
			}
			fields2 := strings.Fields(line2)
			if len(fields2) < 8 {
				continue
			}

			var user2, nice2, system2, idle2, iowait2, irq2, softirq2, steal2 int64
			fmt.Sscanf(line2, "cpu %d %d %d %d %d %d %d %d ", &user2, &nice2, &system2, &idle2, &iowait2, &irq2, &softirq2, &steal2)
			total2 := user2 + nice2 + system2 + idle2 + iowait2 + irq2 + softirq2 + steal2
			idleAll2 := idle + iowait

			totalDiff := total2 - total
			idleDiff := idleAll2 - idleAll

			if totalDiff == 0 {
				return "0%"
			}

			usage := float64(totalDiff-idleDiff) / float64(totalDiff) * 100
			return fmt.Sprintf("%.1f%%", usage)


		}
	}
	return "0%"

}

func getMemoryUsage() string {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "0%"
	}

	lines := strings.Split(string(data), "\n")
	var total, avaiable, buffers, cached int64

	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(line, "MemTotal:%d kB", &total)
		}
		if strings.HasPrefix(line, "MemAvaiable:") {
			fmt.Sscanf(line, "MemAvaiable:%d kB", &avaiable)
		}
		if strings.HasPrefix(line, "Buffers:") {
			fmt.Sscanf(line, "Buffers:%d kB", &buffers)
		}
		if strings.HasPrefix(line, "Cached:") {
			fmt.Sscanf(line, "Cached:%d kB", &cached)
		}
	}

	if total == 0 {
		return "0%"
	}

	used := total - avaiable
	precent := float64(used) / float64(total) * 100

	totalGB := float64(total) / 1024 /1024
	usedGB := float64(used) / 1024 /1024
	return fmt.Sprintf("%.1f%% (%.1f/%.1 GB)", precent, usedGB, totalGB)
}

func getDiskUsage() string {
	cmd := exec.Command("df", "-h", "/")
	out, err := cmd.Output()
	if err != nil {
		return "0%"
	}

	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return "0%"
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return "0%"
	}

	size := fields[0]
	used := fields[2]
	free := fields[1]
	usagePrecent := fields[3]

	return fmt.Sprintf("Дост: %s (Исп: %s / %s, Размер: %s)", usagePrecent, used, size, free)
	//return cleanString(string(out))
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func runCommand(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return cleanString(string(out))
}

func cleanString(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
}

func loadConfig(path string) Config {
	file, err := os.ReadFile(path)
	if err != nil {
		log.Println("!! конфиг не найден, использую дефолтный !!")
		return Config{ServerURL: "http://192.168.1.100:8080/api/agent", Interval: 30}
	}
	var config Config
	if err := json.Unmarshal(file, &config); err != nil {
		log.Println("!! Ошибка парсинга конфига, использую дефолтный !!")
		return Config{ServerURL: "http://192.168.1.100:8080/api/agent", Interval: 30}
	}
	return config
}

// agent Command start

//Проверка и выполнение команды с сервера
func checkAndExecuteCommands(ServerURL string) {
	hostname, _:= os.Hostname()

	url := strings.Replace(ServerURL, "/api/agent", "/api/agent/commands", 1)
	url = fmt.Sprintf("%s?hostname=%s", url, hostname)

	log.Printf("Проверка команд для %s", hostname)
	log.Printf("URL: %s", url)

	client:= &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)



	//resp, err := http.Get(fmt.Sprintf("%s?hostname=%s",
	//	strings.Replace(ServerURL, "/api/agent", "/api/agent/commands", 1), hostname))
	if err != nil {
		log.Printf(" Не могу получить команды: %v", err)
		return
	}
	defer resp.Body.Close()

	log.Printf("Ответ от сервера: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		log.Printf("Ошибка получения команды: %d", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Ошибка чтения ответа: %v", err)
		return
	}

	log.Printf(" Получен ответ: %s", string(body))

	if len(body) == 0 || string(body) == "null" || string(body) == "[]" {
		log.Println("Нет команд")
		return
	}


	var commands []struct {
		ID      string `json:"id"`
		Command string `json:"command"`
		Status  string `json:"status"`
	}

	//if err := json.NewDecoder(resp.Body).Decode(&commands); err != nil {
	//	log.Printf("Ошибка парсинга команд: %v", err)
	//	return
	//}

	if err := json.Unmarshal(body, &commands); err != nil {
		log.Printf("Ошибка парсинга команд: %v", err)
		return
	}

	if len(commands) == 0 {
		log.Println("нет команд в очереди")
		return
	}

	log.Printf("Получено %d команд для выполнения", len(commands))

	for _, cmd := range commands {
		log.Printf("Запуск команды %s: %s", cmd.ID, cmd.Command)
		go executeCommandAndSendResult(ServerURL, cmd.ID, cmd.Command)
	}
}

// выполнение команды и отправка результата
func executeCommandAndSendResult(ServerURL, taskID, command string) {
	log.Printf("Выполнение команды %s: %s", taskID, command)

	//Обновление статуса на running
	sendResult(ServerURL, taskID, "running", "")

	//Выполняем команду
	cmd := exec.Command("sh", "-c", command)
	output, err := cmd.CombinedOutput()

	status := "complited"
	if err != nil {
		status = "failed"
		log.Printf("Ошибка выполнения %s: %v", taskID, err)
	} else {
		log.Printf("Команда %s выполнения успешно",taskID)
	}

	// Отправка результата
	sendResult(ServerURL, taskID, status, string(output))
}

// отправка результата на сервер
func sendResult(ServerURL, taskID, status, result string) {

	url := strings.Replace(ServerURL, "/api/agent", "/api/agent/result", 1)

	data := map[string]interface{}{
		"task_id":  taskID,
		"hostname": getHostname(),
		"status":   status,
		"result":   result,
	}

	jsonData, _ := json.Marshal(data)

	log.Printf("Отправка результата для %s (статус: %s)", taskID, status)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Ошибка создания запроса: %v", err)
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(
		strings.Replace(ServerURL, "/api/agent", "/api/agent/result", 1),
		"application/json",
		bytes.NewBuffer(jsonData),
	)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Expect", "")

	//client := &http.Client{Timeout: 30 * time.Second}
	//resp, err := client.Do(req)
	//if err != nil {
	//	log.Printf("Ошибка отправки результата: %v", err)
	//	return
	//}

	defer resp.Body.Close()

	//body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 200 {
		log.Printf("Результат команды %s отправлен (статус: %s)", taskID, status)
	} else {
		log.Printf("Сервер ответил кодом %d на результат команды %s", resp.StatusCode, taskID)
	}
}

// agent Command end


func getHostname() string {
	name, _ := os.Hostname()
	return name
}

func getAstraVersion() string {

	data3, err := os.ReadFile("/etc/astra_version")
	if err == nil {
		return strings.TrimSpace(string(data3))
	}

	data3, err = os.ReadFile("/etc/os-release")
	if err == nil {
		lines3 := strings.Split(string(data3), "\n")
		for _, line3 := range lines3 {
			if strings.HasPrefix(line3, "PRETTY_NAME=") {
				parts3 := strings.SplitN(line3, "=", 2)
				if len(parts3) == 2 {
					return strings.Trim(parts3[1], `"`)
				}
			}
		}
	}

	return "Unknown"
}

func getServicesStatus(services []string) map[string]string {
	result := make(map[string]string)

	for _, service := range services {

		cmd := exec.Command("systemctl", "is-active", service)
		out, err := cmd.Output()
		if err != nil {
			result[service] = "inactive"
			continue
		}
		status := strings.TrimSpace(string(out))
		if status == "active" {
			result[service] = "active"
		} else if status == "inactive" {
			result[service] = "inactive"
		} else if status == "failed" {
			result[service] = "failed"
		} else {
			result[service] = status
		}
	}
	return result
}

