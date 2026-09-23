package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// структура данных от агената (расширенна)
type SystemInfo struct {
	Hostname  string `json:"Hostname"`
	IP        string `json:"ip_address"`
	OSType    string `json:"os_type"`
	OSVersion string `json:"os_version"`
	Kernel    string `json:"kernel"`
	Uptime    string `json:"uptime"`
	LoadAvg   string `json:"load_avg"`
	CPU       string `json:"cpu_usage"`
	Memory    string `json:"Memory_usage"`
	Disk      string `json:"disk_usage"`
	Timestamp int64 `json:"timestamp"`
	Services  map[string]string `json:"services"`
}

// структура для ответа API
type AgentStatus struct {
	Hostname  string `json:"Hostname"`
	IP        string `json:"ip_address"`
	Memory    string `json:"Memory_usage"`
	Disk      string `json:"disk_usage"`
	CPU       string `json:"cpu_usage"`
	LastSeen  string `json:"last_seen"`
	Status    string `json:"status"`
	Services  map[string]string `json:"services"`
	OSVersion string `json:"os_version"`
}

type CommandTask struct {
	ID          string `json:"id"`
	Hostname    string `json:"hostname"`
	Command     string `json:"command"`
	Status      string `json:"status"` // pending | running | complited | failed
	Result      string `json:"result"`
	CreatedAt   int64  `json:"created_at"`
	ComplitedAt int64  `json:"complited_at"`
}

type CommandRequest struct {
	Hostname         string `json:"hostname"`
	Command          string `json:"command"`
	UseSudo          bool `json:"use_sudo"`
	SudoPassword     string `json:"sudo_password"`
}

const (
	LOG_FILE = "/opt/agent-server/agents_data.log"
	OFFLINE_TIMEOUT = 30 // 30 сек
)
var fileMutex sync.Mutex

// хранилище команд (в памяти + файл)
var (
	commandQueue = make(map[string][]CommandTask) // hostame => []CommandTask
	queueMutex   sync.Mutex
)

func main() {
	// создание директории если её нету
	os.MkdirAll("/opt/agent-server", 0755)


	// 1 эндпоинт для приема данных от агента
	http.HandleFunc("/api/agent", handleAgentData)

	// 3 api для получения данных в json (для скриптов)
	http.HandleFunc("/api/agents", handleAgentsAPI)

	// 5 для удаления из списка
	http.HandleFunc("/api/delete", handleDeleteHost)

	// 6 эндпоинт для отправки команды
	http.HandleFunc("/api/execute", handleExecuteCommand)

	// 7 эндпоинт для получения команды (для агента)
	http.HandleFunc("/api/agent/commands", handleGetCommands)

	// 8 эндпоинт для получения результата выполнения (от агента)
	http.HandleFunc("/api/agent/result", handleCommandResult)

	// 9 эндпоинт для просмотра статуса команд
	http.HandleFunc("/api/commands/status", handleCommandsStatus)

	// 4 Эндпоинт для получения проверки работоспособности
	http.HandleFunc("/health", handleHealth)

	// 2 веб интерфейс для просмотра агентов
	http.HandleFunc("/", handleWebUI)

	// 10 Эндпоинт ui дял управления командами для клиентов
	http.HandleFunc("/commands", handleCommandsUI)
	// загрузка очереди из файла
	loadCommandQueue()

	addr := ":8080"
	log.Printf("Сервер запущен на http://0.0.0.0%s", addr)
	log.Printf("веб интерфейс: http://localhost%s", addr)
	log.Printf("API: http://localhost%s/api/agents", addr)
	log.Printf("commands: http://localhost%s/commands", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

// ОБработка приема данных от агента
func handleAgentData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Проверка что json валидный
	var info SystemInfo
	if err := json.Unmarshal(body, &info); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// логирование получение данных
	log.Printf(" Получены данные от %s (IP: %s, Память: %s, Диск: %s)", info.Hostname, info.IP, info.Memory, info.Disk)

	// сохранение в файл

	fileMutex.Lock()
	err = saveToFile(LOG_FILE, string(body))
	fileMutex.Unlock()
	if err != nil {
		log.Printf("!! Ошибка записи: %v !!", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Успешный ответ
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		Hostname string `json:"hostname"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to ready body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if err:= json.Unmarshal(body, &request); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if request.Hostname == "" {
		http.Error(w, "Hostname is required", http.StatusBadRequest)
		return
	}

	fileMutex.Lock()
	err = deleteHostFromFile(LOG_FILE, request.Hostname)
	fileMutex.Unlock()

	if err != nil {
		log.Printf("!! ошибка удаления %s: %v !!", request.Hostname, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	log.Printf("__ host %s удален из мониторинга __", request.Hostname)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Host deleted"})
}

// вебка по мониторингу
func handleWebUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	agents := getAgentsStatus()

	html := `<!DOCTYPE html>
<html>
<head>
	<title>Мониторинг агентов Astra Linux</title>
	<meta charset="UTF-8">
	<meta http-equiv="refresh" content="30">
	<style>
		* { margin: 0; padding: 0; box-sizing: border-box; }
		body {
			font-family: 'Segoe UI', Arial, sans-serf;
			background: #0f0f1a;
			color: #cdd6f4;
			padding: 20px;
			min-height: 100vh;
		}
		.nav{
			display: flex;
			gap: 10px;
			margin-button: 20px;
			background: #1e1e2e;
			padding: 10px 15px;
			border-radius: 10px;
			border: 1px solid #313244;
		}
		.nav a {
			color: #6c7086;
			text-decoration: none;
			padding: 8px 16px;
			border-radius: 6px;
			font-size: 14px;
			transition: all 0.2s;
		}
		.nav a:hover { background: #313244; color: $cdd6f4;}
		.conteiner {
			max-width: 1400px
			margin: 0px;
		}
		h1 {
			color: #89b4fa;
			font-size: 28px;
			margin-bottom: 10px;
			display: flex;
			align-items: center;
			gap: 10px;
		}
		.subtitle {
			color: #6c7086;
			margin-bottom: 30px;
			font-size: 14px;
		}
		.stats {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
			gap: 15px;
			margin-bottom: 30px;
		}
		.stat-card {
			background: #1e1e2e;
			padding: 15px;
			border-radius: 10px;
			border: 1px solid #313244;
			text-align: center;
		}
		.stat-card .number {
			font-size: 32px;
			font-weight: bold;
			color: #89b4fa;
		}
		.stat-card .label {
			color: #6c7086;
			font-size: 12px;
			text-transform: uppercase;
			letter-spacing: 1px;
			margin-top: 5px;
		}
		.stat-card.online .number { color: #a6e3a1; }
		.stat-card.offline .number { color: #f38ba8; }
		table {
			width: 100%
			border-collapse: collapse;
			background: #1e1e2e;
			border-radius: 10px;
			overflow: hidden;
			border: 1px solid #313244;
		}
		th {
			background: #313244;
			color: #cdd6f4;
			padding: 12px 15px;
			text-align: left;
			font-size: 12px;
			text-transform: uppercase;
			letter-spacing: 0.5px;
		}
		td {
			padding: 12px 15px;
			border-bottom: 1px solid #313244;
			font-size: 14px;
		}
		tr:hover {
			background: #2a2a3e;
		}
		.status-online {
			color: #a6e3a1;
			font-weight: bold;
		}
		.status-ofline {
			color: #f38ba8;
			font-weight: bold;
		}
		.badge-online {
			background: #1e3a2a;
			color: #a6e3a1;
		}
		.badge-offline {
			background: #3a1e1e;
			color: #f38ba8;
		}
		.footer {
			margin-top: 30px;
			color: #6c7086;
			font-size: 12px;
			text-align: center;
		}
		.delete-btn {
			background: none;
			border: none;
			color: #6c7086;
			font-size: 18px;
			cursor: pointer;
			padding: 0px 5px;
			transition: color 0.2s;
			line-height: 1;
		}
		.delete-btn:hover {
			color: #f38ba8;
			transform: scale(1.2);
		}
		.delete-btn:active {
			transform: scale(0.9);
		}
		.toast {
			position: fixed;
			top: 20px;
			right: 20px;
			background: #1e1e2e;
			border: 1px solid #313244;
			padding: 15px 25px;
			border-radius: 8px;
			color: #a6e3a1;
			display: none;
			z-index: 1000;
			box-shadow: 0 4px 15px rgba(0,0,0,0.5);
		}
		.toast.error {
			color: #f38ba8;
			border-color: #f38ba8;
		}

		.service-active { color: #a6e3a1; }
		.service-inactive { color: #f9e2af; }
		.service-failed { color: #f38ba8; }
		.services-cell { color: #f38ba8; transform: scale(1.2); }
		.services-cell .service-item { display: inline-block; margin: 2px 4px; }
	</style>
</head>



<body>
<div class="container">
	<div class="nav">

	<div>
	<h1>Мониторинг агентов Astra Linux</h1>
	<div class="subtitle">Автообновление каждые 30 секунд</div>

	<div class="stats">
		<div class="stat-card">
			<div class="number">` + fmt.Sprintf("%d", len(agents)) + ` </div>
			<div class="label">Всего агентов</div>
		</div>
		<div class="stat-card online">
			<div class="number">` + fmt.Sprintf("%d", countOnline(agents)) + ` </div>
			<div class="lable"> Онлайн</div>
		</div>
		<div class"stat-card offline">
			<div class="number">` + fmt.Sprintf("%d", countOffline(agents)) + ` </div>
			<div class="lable"> Офлфйн</div>
		</div>
		<a href="/">Мониторинг</a>
		<a href="/commands">Команды</a>
	</div>

	<table>
		<thead>
			<tr>
				<th>Хост</th>
				<th>IP-адрес</th>
				<th>Версия Astra</th>
				<th>Память</th>
				<th>Диск</th>
				<th>CPU</th>
				<th>Аптайм</th>
				<th>Послений раз</th>
				<th>Сервисы</th>
				<th>Статус</th>
				<th style="width:50px;">Действие</th>
			</tr>
		</thead>
		<tbody>`

	for _, agent := range agents {
		//statusClass := "status-offline"
		//badgeClass := "badge-offline"
		//statusText := "Офлайн"
		//if agent.Status == "online" {
		//	statusClass := "status-online"
		//	badgeClass := "badge-online"
		//	statusText := "Онлайн"
		//}
		html += `<tr id="row-` + agent.Hostname + `">
			<td><strong>` + agent.Hostname + `</strong></td>
			<td>` + agent.IP + `</td>
			<td>` + agent.OSVersion + `</td>
			<td>` + agent.Memory + `</td>
			<td>` + agent.Disk + `</td>
			<td>` + agent.CPU + `</td>
			<td>` + agent.LastSeen + `</td>
			<td>` + agent.LastSeen + `</td>
			<td class="services-cell">`
		/////
		if len(agent.Services) > 0 {
			for name, status := range agent.Services {
				statusClass := "service-" + status
				html += `<span class="service-item ` + statusClass + `">` + name + `</span> `
			}
		} else {
			html += `<span style="color:#6c7086;">нет данных</span>`
		}


		/////
		html += `</td>
			<td><span class="badge ` //+ badgeClass + `">` + statusText +

		if agent.Status == "online" {
			html += `badge-online"> Онлайн`
		} else {
			html += `badge-offline"> Офланй`
		}
		html += `<span></td>
		<td>
			<button class="delete-btn" onclick="deleteHost('` + agent.Hostname + `')" title="Удалить хост">X</button>
		</td>
		</tr>`
	}

	html += `</tbody></table>
	<div class="footer">Обновление: ` + time.Now().Format("15:04:05") + ` | Astra Linux Monitoring v1.0</div>
</div>

<div id="toast" class="toast"></div>
<script>
function deleteHost(hostname) {
	if (!confirm('Удалить хост"' + hostname + '" из мониторинга?')){
		return;
	}

	fetch('/api/delete', {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json',
		},
		body: JSON.stringify({ hostname: hostname})
	})
	.then(response => response.json())
	.then(data => {
		if (data.status === 'ok') {
			showToast(' Хост ' + hostname + ' Удален');
			const row = document.getElementById('row-' + hostname);
			if (row) {
				row.style.transition = 'opacity 0.3s';
				row.style.opacity = '0';
				setTimeout(() => {
					row.remove();
					location.reload();
				}, 300);
			}
		} else {
			showToast('!! Ошибка удаления !!', true);
		}
	})
	.catch(error => {
		showToast('!! Ошибка: ' + erroe, true);
	});
}

function showToast(message, isError = false) {
	const toast = document.getElementById('toast');
	toast.textContent = message;
	toast.className = 'toast' + (isError ? ' error' : '');
	toast.style.display = 'block';
	setTimeout(() => {
		toast.style.display = 'none';
	}, 3000);
}
</script>

</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, html)
}

// вебка по командам/управлению клинтов
func handleCommandsUI(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html>
<head>
	<title>Команды для управдения агентами Astra Linux</title>
	<meta charset="UTF-8">
	<style>
		* { margin: 0; padding: 0; box-sizing: border-box; }
		body {
			font-family: 'Segoe UI', Arial, sans-serf;
			background: #0f0f1a;
			color: #cdd6f4;
			padding: 20px;
			min-height: 100vh;
		}
		.conteiner {
			max-width: 1200px;
			margin: 0 auto;
		}
		h1 {
			color: #89b4fa;
			margin-bottom: 20px;
		}
		.card {
			background: #1e1e2e;
			border: 1px solid #313244;
			border-radius: 10px;
			padding: 20px;
			margin-bottom: 20px;
		}
		.card h2 {
			color: #89b4fa;
			font-size: 18px;
			margin-bottom: 15px;
		}
		.form-group {
			display: flex;
			gap: 10px;
			margin-button: 10px;
			flew-wrap; wrap;
			align-items: center;
		}
		input, select, textarea {
			background: #313244;
			border: 1px solid #45475a;
			color: #cdd6f4;
			padding: 10px;
			border-radius: 6px;
			flex: 1;
			min-width: 150px;
		}
		.checkbox-group {
			display: flex;
			align-items: center;
			gap: 8px;
			background: #313244;
			padding: 8px 15px;
			border-radius: 6px;
			border: 1px solid #45475a;
		}
		.checkbox-group input[type="checkbox"] {
			width: 18px;
			height: 18px;
			cursor: pointer;
			accent-color: #89b4fa;
			min-width: unset;
			flex: unset;
			margin: 0;
		}
		.checkbox-group label {
			cursor: pointer;
			font-size: 14px;
			color: #cdd6f4;
			user-select: none;
		}
		.password-group {
			display: flex;
			align-items: center;
			gap: 8px;
			background: #313244;
			paddingL 8px 15px;
			border-radius: 6px;
			border: 1px solid #45475a;
			flex: 0 1 auto;
		}
		.password-group input[type="password"] {
			background: transpanent;
			border: none;
			color: #cdd6f4;
			padding: 0;
			min-wodth: 120px;
			flex: unset;
		}
		.password-group input[type="password"]:focus {
			outline: none;
		}
		button {
			background: #89b4fa;
			border: none;
			color: #1e1e2e;
			padding: 10px 20px;
			border-radius: 6px;
			font-weight: bold;
			cursor: pointer;
			transition: opacity 0.2s;
		}
		button:hover { opacity: 0.8; }
		button.danger { background: #f38ba8; }
		.log {
			background: #0f0f1a;
			border: 1px solid #313244;
			padding: 10px;
			border-radius: 6px;
			font-family: monospace;
			font-size: 12px;
			max-height: 500px;
			overflow-y: auto;
			cursor: pointer;
			transition: opacity 0.2s;
			white-space: pre-warp;
			display: flex;
			flex-direction: column;
		}
		.log-entry {
			border-button: 1px solid #1e1e2e;
		}
		.log-entry:last-child {
			padding: 2px 8px;
			border-radius: 4px;
			font-size: 11px;
			font-weight: bold;
		}

		.command-status {
			padding: 2px 10px;
			border-radius: 4px;
			font-size: 11px;
			font-weight: bolt;
		}
		.status-pending { background: #f9e2af; color: #1e1e2e;}
		.status-running { background: #89b4fa; color: #1e1e2e;}
		.status-complited { background: #a6e3a1; color: #1e1e2e;}
		.status-failed { background: #f38ba8; color: #1e1e2e;}
		.host-select {
			display: flex;
			gap: 10px;
			flex-wrap: wrap;
		}
		.host-tag {
			background: #313244;
			padding: 2px 8px;
			border-radius: 4px;
			cursor: pointer;
			font-size: 12px;
			display: inline-block;
			margin: 2px;
		}
		.host-tag:hover { background: #45475a; }
		.result-box {
			background: #0f0f1a;
			padding: 10px;
			border-radius: 4px;
			margin-top: 10px;
			font-family: monospace;
			font-size: 13px;
			white-space: pre-wrap;
			border-left: 3px solid #89b4fa;
		}
		.nav{
			display: flex;
			gap: 10px;
			margin-button: 20px;
			background: #1e1e2e;
			padding: 10px 15px;
			border-radius: 10px;
			border: 1px solid #313244;
		}
		.nav a {
			color: #6c7086;
			text-decoration: none;
			padding: 8px 16px;
			border-radius: 6px;
			font-size: 14px;
			transition: all 0.2s;
		}
		.nav a:hover { background: #313244; color: $cdd6f4;}
		.refresh-btn {
			background: #313244;
			color: #cdd6f4;
			padding: 5px 15px;
			border-radius: 4px;
			border: none;
			cursor: pointer;
			font-size: 14px;
		}
		.refresh-btn:hover { background: #45475a; }
		.sudo-badge {
			background: #f38ba8;
			color: #1e1e2e;
			padding: 1px 6px;
			border-radius: 3px;
			font-size: 10px;
			font-weight: bold;
			margin-left: 5px;
		}
	</style>
</head>
<body>
	<div class="container">
		<h1> Удалеенное выполнение команд</h1>
		<div class="card">
			<h2>Отправить команду</h2>
			<div class="form-group">
				<div class="form-group">
					<div class="nav">
						<a href="/">Мониторинг</a>
						<a href="/commands">Команды</a>
					</div>
				</div>
				<input type="text" id="hostname" placeholder="Имя хоста (например: server10043)" list="hosts-list">
				<datalist id="hosts-list"></datalist>
				<input type="text" id="command" placeholder="Команда (например apt update -y)" style="flex:2;">

				<div class="checkbox-group">
					<input type="checkbox" id="useSudo">
					<label for="useSudo">sudo</label>
				</div>

				<div class="password-group" id="passwordGroup" style="display:none;">
					<input type="password" id="sudoPassword" placeholder="Пароль sudo">
				</div>
				<button onclick="executeCommand()">Выполнить</button>
			</div>



			<div style="margin-top:10px; font-size:12; color:#6c7086;">
				Доступны новые команды: <span class="host-tag" onclick="setCommand('apt update -y')">apt update</span>
				<span class="host-tag" onclick="setCommand('apt upgrade -y')">apt upgrade</span>
				<span class="host-tag" onclick="setCommand('systemctl status')">systemctl status</span>
				<span class="host-tag" onclick="setCommand('df -h')">df -h</span>
				<span class="host-tag" onclick="setCommand('free -h')">free -h</span>
			</div>
		</div>

		<div class="card">
			<h2>Статус команд <button class="refresh-btn" onclick="loadStatus()">Обновить</button></h2>
			<div id="commandStatus" class="log">Загрузка...</div>
		</div>
	</div>

	<script>


	document.addEventListener('DOMContentLoaded', function() {
			const useSudoCheckbox = document.getElementById('useSudo');
			if (useSudoCheckbox) {
				useSudoCheckbox.addEventListener('change', function() {
					const passwordGroup = document.getElementById('passwordGroup');
					const sudoPassword = document.getElementById('sudoPassword');
					if (this.checked) {
						passwordGroup.style.display = 'flex';
						sudoPassword.focus();
					} else {
						passwordGroup.style.display = 'none';
						sudoPassword.value = '';
					}
			});
		}
	});

	// загрузка списка хостов
	async function loadHosts() {
		try {
			const response = await fetch('/api/agents');
			const data = await response.json();
			const datalist = document.getElementById('hosts-list');
			datalist.innerHTML = '';
			if (data.agents) {
				data.agents.forEach(agent => {
					const option = document.createElement('option');
					option.value = agent.hostname;
					datalist.appendChild(option);
				});
			}
		} catch(e) {
			console.error('Ошибка загрузки хостов:', e);
		}
	}

	// Отправка команды
	async function executeCommand() {
		const hostname = document.getElementById('hostname').value.trim();
		const command = document.getElementById('command').value.trim();
		const useSudo = document.getElementById('useSudo').checked;
		const sudoPassword = document.getElementById('sudoPassword').value;

		console.log('Отправка команды:');
		console.log('	Хост:', hostname);
		console.log('	Команда:', command);
		console.log('	sudo:', useSudo);
		console.log('	Пароль:', sudoPassword ? '*** (заполнен)' : 'пусто');


		if (!hostname || !command) {
			alert('Введите хост и команду');
			return;
		}

		if (useSudo && !sudoPassword) {
			alert("Введите пароль sudo");
			document.getElementById("sudoPassword").focus();
			return;
		}

		if (!confirm('Выполнить на' + hostname + ':\n' + command)) {
			return;
		}

		try {
			const response = await fetch('/api/execute', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					hostname: hostname,
					command: command,
					use_sudo: useSudo,
					sudo_password: sudoPassword || ""
				})
			});
			const data = await response.json();

			if (data.status === 'ok') {
				alert('Команда отправлена! ID: ' + data.id);
				document.getElementById("command").value = "";
				document.getElementById("useSudo").checked = false;
				document.getElementById("passwordGroup").style.display = "none";
				document.getElementById("sudoPassword").value = "";

				loadStatus();
			} else {
				alert('Ошибка: ' + (data.message || 'Неизвестная ошибка'));
			}
		} catch(e) {
			alert('Ошибка: ' + e)
		}
	}

	// Загрузка статуса команд
	async function loadStatus() {
		const container = document.getElementById('commandStatus');

		if (!container) {
			console.error('Элемент commandStatus не найден');
			return;
		}

		try {
			container.innerHTML = 'зАгРуЗкА...';
			const response = await fetch('/api/commands/status');

			if (!response.ok) {
				throw new Error('HTTP error! status: ' + response.status);
			}

			const data = await response.json();
			console.log('Получены данные:', data);

			let html = '';
			const hostnames = Object.keys(data);

			if (hostname.length === 0) {
				html = 'нет команд в очереди';
			} else {

				hostnames.forEach(host => {
					html += '\n* <strong>' + host + '</strong>\n';
					const tasks = data[host];
					if (!tasks || tasks.length === 0) {
						html += ' (нету команд)\n';
					} else {
						const reversedTasks = tasks.slice().reverse();
						reversedTasks.forEach(function(task) {
							const statusClass = 'status-' + task.status;
							const statusText = {
								'pending': 'Ожидание',
								'running': 'Выполнение',
								'completed': 'Завершена',
								'failed': 'Ошибка'
							}[task.status] || task.status;

							html += ' [' + task.id + '] ' + task.command + '\n';
							html += ' <br> Статус: <span class="command-status ' + statusClass + '">' + statusText + '</span>\n';

							if (task.result && task.result !== '') {
							html += ' <br> Результат:\n' + task.result.replace(/\n/g, '\n	') + '\n';
							}

							if (task.created_at) {
							html += ' <br> Создано: ' + new Date(task.created_at * 1000).toLocaleString() + '\n';
							}

							if (task.complited_at && task.complited_at > 0) {
								html += ' <br> Завершено: ' + new Date(task.complited_at * 1000).toLocaleString() + '\n <br>~~~<br>~~~<br>';
							}

							html += '  ---\n';
						});
					}
					html += '\n';
				});
			}

			container.innerHTML = html;
			// Автоскролл вниз
			container.scrollBottom = container.scrollHeight;
		} catch(e) {
			console.error('Ошибка загрузки:', e);
			container.innerHTML = 'Ошибка загрузки: ' + e.message;
		}
	}

	// Установка команды из тега
	async function setCommand(cmd) {
		document.getElementById('command').value = cmd;
	}


	// Автообновление каждые 10 секунд
		loadHosts();
		loadStatus();
		setInterval(loadStatus, 10000);

</script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, html)
}

// API для получения данных в json
func handleAgentsAPI(w http.ResponseWriter, r *http.Request) {
	agents := getAgentsStatus()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total":     len(agents),
		"online":    countOnline(agents),
		"offline":   countOffline(agents),
		"agents":    agents,
		"timestamp": time.Now().Unix(),
	})
}

// health check
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// отправка команды на выполнение
func handleExecuteCommand(w http.ResponseWriter, r *http.Request) {

	log.Printf(" Запрос на выполнение команды: %s %s", r.Method, r.URL.Path)

	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		log.Printf(" Неправильный метод: %s, ожидается POST", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req CommandRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Hostname == "" || req.Command == "" {
		http.Error(w, "Hostname and command are required", http.StatusBadRequest)
		return
	}

	finalCommand := req.Command
	if req.UseSudo && req.SudoPassword != "" {
		escapedCommand := strings.ReplaceAll(req.Command, `"`, `\\"`)
		finalCommand = fmt.Sprintf(`echo "%s" | sudo -S sh -c "%s"`, req.SudoPassword, escapedCommand)
		log.Printf(" Команда с sudo для %s: %s", req.Hostname, finalCommand)
	}


	// создание задачи
	task := CommandTask{
		ID:          generateID(),
		Hostname:    req.Hostname,
		Command:     finalCommand,
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
	}

	// сохранение в очередь
	queueMutex.Lock()
	commandQueue[req.Hostname] = append(commandQueue[req.Hostname], task)
	queueMutex.Unlock()

	// сохранение в файл
	saveCommandQueue()

	log.Printf("Команда для %s: %s (ID: %s)", req.Hostname, req.Command, task.ID)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"id":     task.ID,
		"message": "Command queued for execution",
	})
}

// получение комманд для агента(GET)
func handleGetCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hostname := r.URL.Query().Get("hostname")
	if hostname == "" {
		http.Error(w, "Hostname is required", http.StatusBadRequest)
		return
	}

	queueMutex.Lock()
	tasks := commandQueue[hostname]
	//фильтр на отдачу только задач с статусом pending
	pending := []CommandTask{}
	for _, task := range tasks {
		if task.Status == "pending" {
			pending = append(pending, task)
		}
	}
	queueMutex.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pending)
}

// прием результата выполнение команды от агента
func handleCommandResult(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		log.Printf(" Неправильный метод: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var result struct {
		TaskID   string `json:"task_id"`
		Hostname string `json:"hostname"`
		Status   string `json:"status"`
		Result   string `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	queueMutex.Lock()
	tasks := commandQueue[result.Hostname]
	for i, task := range tasks {
		if task.ID == result.TaskID {
			tasks[i].Status = result.Status
			tasks[i].Result = result.Result
			tasks[i].ComplitedAt = time.Now().Unix()
			log.Printf("Обновлена задача %s: статус %s", result.TaskID, result.Status)
			break
		}
	}
	commandQueue[result.Hostname] = tasks
	queueMutex.Unlock()

	saveCommandQueue()

	log.Printf("Команда %s выполнена на %s: %s", result.TaskID, result.Hostname, result.Status)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// получение статуса всех команд
func handleCommandsStatus(w http.ResponseWriter, r *http.Request) {
	queueMutex.Lock()
	defer queueMutex.Unlock()

	//если очередь пустая выводит пустой объект
	if len(commandQueue) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{})
		return
	}


	// группировка по хостам
	response := make(map[string][]CommandTask)
	for host, tasks := range commandQueue {
		// возвращение последние 20 задач для каждого хоста
		//lastTasks := tasks
		if len(tasks) > 50 {
			response[host] = tasks[len(tasks)-50:]
		} else {
			response[host] = tasks
		}
	}

	w.Header().Set("Content=Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Ошибка отправки статуса: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}


// получение статусавсех агентов
func getAgentsStatus() []AgentStatus {
	fileMutex.Lock()
	data, err := os.ReadFile(LOG_FILE)
	fileMutex.Unlock()

	if err != nil {
		return []AgentStatus{}
	}

	lines := strings.Split(string(data), "\n")
	agentsMap := make(map[string]AgentStatus)
	now := time.Now().Unix()

	for _, line := range lines {
		if line == "" {
			continue
		}

		var info SystemInfo
		if err := json.Unmarshal([]byte(line), &info); err !=nil {
			continue
		}

		// Определение статуса
		status := "offline"
		if now-info.Timestamp < OFFLINE_TIMEOUT {
			status = "online"
		}

		// сохраниене только последней записи для каждого хоста
		agentsMap[info.Hostname] = AgentStatus{
			Hostname:  info.Hostname,
			IP:        info.IP,
			Memory:    info.Memory,
			Disk:      info.Disk,
			CPU:       info.CPU,
			LastSeen:  time.Unix(info.Timestamp, 0).Format("15:04:05"),
			Status:    status,
			Services:  info.Services,
			OSVersion: info.OSVersion,
		}
	}

	// Преобразуем Map в Slice
	result := []AgentStatus{}
	for _, agent := range agentsMap {
		result = append(result, agent)
	}
	return result
}

//удаление всех записей с указаным хостом из файлов
func deleteHostFromFile(path, hostname string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	newLines := []string{}

	for _, line := range lines {
		if line == "" {
			continue
		}

		var info SystemInfo
		if err := json.Unmarshal([]byte(line), &info); err != nil {
			newLines = append(newLines, line)
			continue
		}

		if info.Hostname != hostname {
			newLines = append(newLines, line)
		}
	}

	newContent := strings.Join(newLines, "\n")
	if len(newLines) > 0 {
		newContent += "\n"
	}

	return os.WriteFile(path, []byte(newContent), 0644)
}


// Вспомогательные функции
func saveToFile(path, data string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(data + "\n")
	return err
}

func countOnline(agents []AgentStatus) int {
	count := 0
	for _, a := range agents {
		if a.Status == "online" {
			count++
		}
	}
	return count
}

func countOffline(agents []AgentStatus) int {
	count := 0
	for _, a := range agents {
		if a.Status == "offline" {
			count++
		}
	}
	return count
}

func generateID() string {
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), strings.ToLower(string([]byte{byte('a' + time.Now().Nanosecond()%26)})))
}

func saveCommandQueue() {
	queueMutex.Lock()
	defer queueMutex.Unlock()

	data, err := json.Marshal(commandQueue)
	if err != nil {
		log.Printf("!! Ошибка сохранение очереди: %v !!", err)
		return
	}
	if err := os.WriteFile("/opt/agent-server/commands.json", data, 0644); err != nil {
		log.Printf("!! Ошибка записи файла команд: %v !!", err)
	}


	//data, err := json.Marshal(commandQueue)
	//if err != nil {
	//	log.Printf("!! Ошибка сохранение очереди: %v !!", err)
	//	return
	//}
	//os.WriteFile("/opt/agent-server/commands.json", data, 0644)
}

func loadCommandQueue() {
	data, err := os.ReadFile("/opt/agent-server/commands.json")
	if err !=nil {
		log.Printf(" Файл команд не найденб создаем новый")
		commandQueue = make(map[string][]CommandTask)
	}

	if err := json.Unmarshal(data, &commandQueue); err !=nil {
		log.Printf(" Ошибка загрузки очереди: %v", err)
		commandQueue = make(map[string][]CommandTask)
	}
}

//if (!confirm(`Выполнить на ${hostname}:\n${command}`)) {
	//	return;
	//}
