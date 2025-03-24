package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/joho/godotenv"
)

type ZabbixResponse struct {
	Jsonrpc string      `json:"jsonrpc"`
	Result  interface{} `json:"result"`
	Error   ZabbixError `json:"error"`
	Id      int         `json:"id"`
}

type ZabbixError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

type Host struct {
	XMLName      xml.Name    `xml:"host"`
	HostID       string      `xml:"hostid"`
	HostName     string      `xml:"name"`
	IPAddress    string      `xml:"ip"`
	Status       string      `xml:"status"`
	Availability string      `xml:"availability"`
	Notes        string      `xml:"notes,omitempty"`
	Groups       []Group     `xml:"groups>group"`
	Templates    []Template  `xml:"templates>template"`
	Interfaces   []Interface `xml:"interfaces>interface"`
}

type Group struct {
	GroupID string `xml:"groupid"`
	Name    string `xml:"name"`
}

type Template struct {
	TemplateID string `xml:"templateid"`
	Name       string `xml:"name"`
}

type Interface struct {
	InterfaceID string `xml:"interfaceid"`
	IPAddress   string `xml:"ip"`
	Port        string `xml:"port"`
	Type        string `xml:"type"`
}

type Metric struct {
	ItemID string `json:"itemid"`
	Name   string `json:"name"`
	Key    string `json:"key"`
	Value  string `json:"value"`
}

type Trigger struct {
	TriggerID   string `json:"triggerid"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	Status      string `json:"status"`
}

// saveToXML сохраняет данные в файл XML
func saveToXML(filename string, data interface{}) {
	xmlData, err := xml.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Fatalf("Ошибка при преобразовании данных в XML: %v", err)
	}

	// Добавляем заголовок XML перед сохранением
	err = os.WriteFile(filename, []byte(xml.Header+string(xmlData)), 0644)
	if err != nil {
		log.Fatalf("Ошибка при сохранении XML-файла %s: %v", filename, err)
	}
}

// saveToJSON сохраняет данные в файл JSON
func saveToJSON(filename string, data interface{}) {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Fatalf("Ошибка при преобразовании данных в JSON: %v", err)
	}

	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		log.Fatalf("Ошибка при сохранении JSON-файла %s: %v", filename, err)
	}
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Ошибка загрузки .env файла")
	}

	zabbixUser := os.Getenv("ZBX_USER")
	zabbixPassword := os.Getenv("ZBX_PASSWD")
	zabbixServer := os.Getenv("ZBX_URL")
	exportDir := os.Getenv("EXPORT_DIRECTORY")

	client := resty.New()
	client.SetTimeout(30 * time.Second)

	authResp, err := client.R().
		SetHeader("Content-Type", "application/json-rpc").
		SetBody(map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "user.login",
			"params": map[string]string{
				"user":     zabbixUser,
				"password": zabbixPassword,
			},
			"id": 1,
		}).
		Post(zabbixServer)

	if err != nil {
		log.Fatalf("Ошибка при запросе к Zabbix API: %v", err)
	}

	var authResult ZabbixResponse
	err = json.Unmarshal(authResp.Body(), &authResult)
	if err != nil {
		log.Fatalf("Ошибка при разборе ответа от Zabbix API: %v", err)
	}

	if authResult.Error.Code != 0 {
		log.Fatalf("Ошибка аутентификации: %s", authResult.Error.Message)
	}

	zabbixToken, ok := authResult.Result.(string)
	if !ok || zabbixToken == "" {
		log.Fatalf("Ошибка: Токен аутентификации не получен")
	}

	exportHostsWithDetails(client, zabbixToken, zabbixServer, exportDir)

	fmt.Println("Экспорт данных завершен.")
}

func exportHostsWithDetails(client *resty.Client, token, server, exportDir string) {
	resp, err := client.R().
		SetHeader("Content-Type", "application/json-rpc").
		SetBody(map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "host.get",
			"params": map[string]interface{}{
				"output":           "extend",
				"selectGroups":     "extend",
				"selectTemplates":  "extend",
				"selectInterfaces": []string{"interfaceid", "ip", "port", "type"},
			},
			"auth": token,
			"id":   1,
		}).
		Post(server)

	if err != nil {
		log.Fatalf("Ошибка при запросе к Zabbix API: %v", err)
	}

	var result ZabbixResponse
	err = json.Unmarshal(resp.Body(), &result)
	if err != nil {
		log.Fatalf("Ошибка при разборе ответа от Zabbix API: %v", err)
	}

	if result.Result == nil || len(result.Result.([]interface{})) == 0 {
		log.Println("Нет доступных хостов для экспорта.")
		return
	}

	for _, item := range result.Result.([]interface{}) {
		data := item.(map[string]interface{})
		availability := "Unknown"
		if data["available"].(string) == "1" {
			availability = "Available"
		} else if data["available"].(string) == "0" {
			availability = "Unavailable"
		}

		host := Host{
			HostID:       data["hostid"].(string),
			HostName:     data["name"].(string),
			IPAddress:    data["interfaces"].([]interface{})[0].(map[string]interface{})["ip"].(string),
			Status:       data["status"].(string),
			Availability: availability,
		}

		if desc, ok := data["description"].(string); ok {
			host.Notes = desc
		}

		if groups, ok := data["groups"].([]interface{}); ok {
			for _, group := range groups {
				groupData := group.(map[string]interface{})
				host.Groups = append(host.Groups, Group{
					GroupID: groupData["groupid"].(string),
					Name:    groupData["name"].(string),
				})
			}
		}

		if templates, ok := data["parentTemplates"].([]interface{}); ok {
			for _, template := range templates {
				templateData := template.(map[string]interface{})
				host.Templates = append(host.Templates, Template{
					TemplateID: templateData["templateid"].(string),
					Name:       templateData["name"].(string),
				})
			}
		}

		// Создаём директорию для хоста
		hostDir := fmt.Sprintf("%s/hosts/%s", exportDir, host.HostName)
		os.MkdirAll(hostDir, 0755)

		// Сохраняем данные хоста в XML
		saveToXML(fmt.Sprintf("%s/host.xml", hostDir), host)

		// Экспортируем метрики и триггеры в JSON
		exportMetricsForHost(client, token, server, hostDir, host.HostID)
		exportTriggersForHost(client, token, server, hostDir, host.HostID)
	}
	fmt.Println("Экспорт хостов завершен.")
}

func exportMetricsForHost(client *resty.Client, token, server, hostDir, hostID string) {
	resp, err := client.R().
		SetHeader("Content-Type", "application/json-rpc").
		SetBody(map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "item.get",
			"params": map[string]interface{}{
				"output":  "extend",
				"hostids": hostID,
			},
			"auth": token,
			"id":   1,
		}).
		Post(server)

	if err != nil {
		log.Printf("Ошибка при получении метрик для хоста %s: %v", hostID, err)
		return
	}

	var result ZabbixResponse
	err = json.Unmarshal(resp.Body(), &result)
	if err != nil {
		log.Printf("Ошибка при разборе метрик хоста %s: %v", hostID, err)
		return
	}

	metrics := []Metric{}
	for _, item := range result.Result.([]interface{}) {
		data := item.(map[string]interface{})
		metric := Metric{
			ItemID: data["itemid"].(string),
			Name:   data["name"].(string),
			Key:    data["key_"].(string),
			Value:  data["lastvalue"].(string),
		}
		metrics = append(metrics, metric)
	}

	saveToJSON(fmt.Sprintf("%s/metrics.json", hostDir), metrics)
	fmt.Printf("Метрики успешно экспортированы для хоста %s в %s/metrics.json\n", hostID, hostDir)
}

func exportTriggersForHost(client *resty.Client, token, server, hostDir, hostID string) {
	resp, err := client.R().
		SetHeader("Content-Type", "application/json-rpc").
		SetBody(map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "trigger.get",
			"params": map[string]interface{}{
				"output":  "extend",
				"hostids": []string{hostID}, // hostids должен быть массивом строк
			},
			"auth": token,
			"id":   1,
		}).
		Post(server)

	if err != nil {
		log.Printf("Ошибка при запросе триггеров для хоста %s: %v", hostID, err)
		return
	}

	// Логируем ответ для диагностики
	fmt.Printf("Ответ от API для триггеров хоста %s: %s\n", hostID, string(resp.Body()))

	var result ZabbixResponse
	err = json.Unmarshal(resp.Body(), &result)
	if err != nil {
		log.Printf("Ошибка при разборе ответа триггеров для хоста %s: %v", hostID, err)
		return
	}

	// Проверяем, есть ли данные
	if result.Result == nil {
		log.Printf("Нет триггеров для хоста %s.\n", hostID)
		return
	}

	// Сохраняем триггеры
	triggers := []Trigger{}
	for _, trigger := range result.Result.([]interface{}) {
		data := trigger.(map[string]interface{})
		tr := Trigger{
			TriggerID:   getStringFromMap(data, "triggerid"),
			Description: getStringFromMap(data, "description"),
			Priority:    getStringFromMap(data, "priority"),
			Status:      getStringFromMap(data, "status"),
		}
		triggers = append(triggers, tr)
	}

	// Сохраняем триггеры в JSON-файл
	saveToJSON(fmt.Sprintf("%s/triggers.json", hostDir), triggers)
	fmt.Printf("Триггеры успешно экспортированы для хоста %s в %s/triggers.json\n", hostID, hostDir)
}

// Вспомогательная функция для безопасного извлечения строковых данных из карты
func getStringFromMap(data map[string]interface{}, key string) string {
	if val, ok := data[key]; ok {
		if strVal, ok := val.(string); ok {
			return strVal
		}
	}
	return ""
}
