package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/emirpasic/gods/sets/treeset"
	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/mmcdole/gofeed"
	"hash/fnv"
	"html"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const d = 1000000000

var bot *tgbotapi.BotAPI
var parser *gofeed.Parser
var feeds []*gofeed.Feed
var w *os.File

type db struct {
	Hashes []uint64
	Urls   []string
	Ids    []int64
}

var database db

func lookup(envName string) string {
	res, ok := os.LookupEnv(envName)
	if !ok {
		log.Fatal("Please set the " + envName + " environmental variable.")
	}
	return res
}

func replacement(r rune) string {
	var res string
	if (8210 <= int(r) && int(r) < 8214) || int(r) == 11834 || int(r) == 11835 || int(r) == 45 || int(r) == 32 {
		res = "_"
	} else if r == '!' || r == '?' || r == '(' || r == ')' || r == '\'' || r == '"' || r == '«' || r == '»' {
		res = ""
	} else if r == '&' || r == '+' {
		res = "_"
	} else if r == '#' {
		res = "sharp"
	} else if r == '.' {
		res = "dot"
	} else {
		res = string(unicode.ToLower(r))
	}
	return res
}

func toHashTag(category string) string{
	res := "#"
	category = strings.ReplaceAll(category, "*nix", "unix")
	category = strings.ReplaceAll(category, "c++", "cpp")
	for i, r := range category {
        x := replacement(r)
        if (x == "_" && res[-1] != "_") || x != "_" {
            res += x
        }
	}
	return res
}

func formatCategories(item *gofeed.Item) string {
	n := len(item.Categories)
	categories := make([]string, n)
	s := treeset.NewWithStringComparator()
	for i := 0; i < n; i += 1{
		categories[i] = toHashTag(item.Categories[i])
	}
	for i := 0; i < n; i += 1 {
		if !s.Contains(categories[i]) {
			s.Add(categories[i])
		}
	}
	values := s.Values()
	res := ""
	for index := 0; index < len(values); index += 1 {
		res += values[index].(string) + " "
	}
	return res
}

func formatItem(feed *gofeed.Feed, itemNumber int) *string {
	item := feed.Items[itemNumber]
	res := "[" + html.EscapeString(feed.Title) + "]\n<b>" +
		html.EscapeString(item.Title) + "</b>\n" +
		html.EscapeString(formatCategories(item)) + "\n\n" +
		"<a href=\"" + html.EscapeString(item.Link) + "\">Читать</a>"
	return &res
}

func sendItem(chatID int64, feed *gofeed.Feed, itemNumber int) bool {
	if itemNumber < len(feed.Items) {
		msg := tgbotapi.NewMessage(chatID, *formatItem(feed, itemNumber))
		msg.ParseMode = "HTML"
		_, _ = bot.Send(msg)
		return true
	} else {
		return false
	}
}

func removeGetArgs(u string) string {
	parsed, _ := url.Parse(u)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func filter(item *gofeed.Item) bool {
	hasher := fnv.New64a()
	_, _ = io.WriteString(hasher, removeGetArgs(item.Link))
	hash := hasher.Sum64()
	res := true
	for i := len(database.Hashes) - 1; i >= 0; i -= 1 {
		if database.Hashes[i] == hash {
			res = false
			break
		}
	}
	if res {
		database.Hashes = append(database.Hashes, hash)
		_, _ = w.WriteString("+ h " + strconv.FormatUint(hash, 10) + "\n")
		_ = w.Sync()
		return true
	}
	return false
}

func update() {
	for i := 0; i < len(database.Urls); i += 1 {
		var err error
		feeds[i], err = parser.ParseURL(database.Urls[i])
		if err == nil {
			for itemNumber := len(feeds[i].Items) - 1; itemNumber >= 0; itemNumber -= 1 {
				if filter(feeds[i].Items[itemNumber]) {
					for idNumber := 0; idNumber < len(database.Ids); idNumber += 1 {
						sendItem(database.Ids[idNumber], feeds[i], itemNumber)
					}
				}
			}
		} else {
			log.Println(database.Urls[i] + " пидарасы!")
		}
	}
}

func evolve() {
	data, err := ioutil.ReadFile("db.json")
	if err == nil {
		err = json.Unmarshal(data, &database)
	}
	n := len(database.Urls)
	file, err := os.Open("evolution.txt")
	if err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			splitted := strings.Split(scanner.Text(), " ")
			if splitted[0] == "+" {
				if splitted[1] == "h" {
					res, _ := strconv.ParseUint(splitted[2], 10, 64)
					database.Hashes = append(database.Hashes, res)
				} else if splitted[1] == "u" {
					database.Urls = append(database.Urls, splitted[2])
					n += 1
				} else if splitted[1] == "i" {
					res, _ := strconv.ParseInt(splitted[2], 10, 64)
					database.Ids = append(database.Ids, res)
				}
			}
		}
	}
	feeds = make([]*gofeed.Feed, n)
	data, err = json.Marshal(database)
	file, err = os.Create("db.json")
	if err != nil {
		log.Panic(err)
	}
	_, _ = file.Write(data)
	_ = file.Sync()
}

func startPolling() {
	for {
		time.Sleep(d)
		go update()
	}
}

func updateHandler() {
	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 120
	updates, err := bot.GetUpdatesChan(updateConfig)
	if err != nil {
		log.Panic(err)
	}
	for update := range updates {
		chatId := update.Message.Chat.ID
		if update.Message.IsCommand() {
			args := update.Message.CommandArguments()
			switch update.Message.Command() {
			case "add_chat_id":
				res, err := strconv.ParseInt(args, 10, 64)
				if err == nil {
					msg, err := bot.Send(tgbotapi.NewMessage(res, "Test"))
					if err == nil {
						fmt.Println(msg.MessageID)
						tgbotapi.NewDeleteMessage(msg.Chat.ID, msg.MessageID)
						database.Ids = append(database.Ids, res)
						_, _ = w.WriteString("+ i " + strconv.FormatInt(res, 10) + "\n")
						_ = w.Sync()
						_, _ = bot.Send(tgbotapi.NewMessage(chatId, "Done!"))
					} else {
						_, _ = bot.Send(tgbotapi.NewMessage(chatId, "Check that bot has access to this chat"))
					}
				}
			case "start":
                fmt.Println("Debug5.2")
				msg := tgbotapi.NewMessage(chatId, "Hi!")
				_, _ = bot.Send(msg)
			case "get_ith":
                fmt.Println("Debug5.3")
				splitted := strings.Split(args, " ")
				if len(splitted) > 1 {
					res1, err1 := strconv.Atoi(splitted[0])
					res2, err2 := strconv.Atoi(splitted[1])
					if err1 == nil && err2 == nil && res1 < len(feeds) && sendItem(chatId, feeds[res1], res2) {
					} else {
						msg := tgbotapi.NewMessage(chatId, "Check arguments")
						_, _ = bot.Send(msg)
					}
				} else {
					_, _ = bot.Send(tgbotapi.NewMessage(chatId, "Not a number"))
				}
			case "add_feed":
                fmt.Println("Debug5.4")
				if _, err := url.Parse(args); err == nil {
					feed, err := parser.ParseURL(args)
					if err == nil {
						feeds = append(feeds, feed)
						database.Urls = append(database.Urls, args)
						_, _ = w.WriteString("+ u " + args + "\n")
						_ = w.Sync()
						_, _ = bot.Send(tgbotapi.NewMessage(chatId, "Done! New feed index: " + strconv.Itoa(len(feeds) - 1)))
					} else {

					}
				} else {
					msg := tgbotapi.NewMessage(chatId, "Please send me an URL")
					_, _ = bot.Send(msg)
				}
			}
		}
	}
}

func main() {
	proxyURL, err := url.Parse(lookup("HTTP_PROXY"))
	if err != nil {
		log.Fatal("Invalid HTTP proxy URL")
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	bot, err = tgbotapi.NewBotAPIWithClient(lookup("TOKEN"), client)
	if err != nil {
		log.Panic(err)
	}
	database = db{}
	feeds = []*gofeed.Feed{}
	parser = gofeed.NewParser()
	bot.Debug = false
	evolve()
	w, err = os.Create("evolution.txt")
	go updateHandler()
	startPolling()
}
