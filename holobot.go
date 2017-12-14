// Copyright (c) 2016 Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package main

import (
	"fmt"
	"github.com/mattermost/mattermost-server/model"
	"os"
	"os/signal"
	"regexp"
	"strings"
)

type Config struct {
	LongName     string
	UserName     string
	UserEmail    string
	UserFirst    string
	UserLast     string
	UserPassword string
	TeamName     string
	LogChannel   string
	Domain       string
}

var config Config
var client *model.Client4
var webSocketClient *model.WebSocketClient

var botUser *model.User
var botTeam *model.Team
var debuggingChannel *model.Channel

type ActionHandler func(event *model.WebSocketEvent) error

type Action struct {
	Name    string
	Event   string
	Handler ActionHandler
}

var actions []Action

type CommandHandler func(event *model.WebSocketEvent, post *model.Post) error

type Command struct {
	Name        string
	Description string
	Handler     CommandHandler
}

var commands []Command

// Documentation for the Go driver can be found
// at https://godoc.org/github.com/mattermost/platform/model#Client
func main() {
	println(config.LongName)

	// load the config
	f, err := os.Open("config.yaml")
	if err != nil {
		fmt.Printf("couldn't open config file: %v\n", err)
		return
	}
	defer f.Close()
	err = Decode(f, "yaml", &config)
	if err != nil {
		fmt.Printf("couldn't decode config file: %v\n", err)
		return
	}

	SetupGracefulShutdown()

	client = model.NewAPIv4Client("https://" + config.Domain)

	// Lets test to see if the mattermost server is up and running
	MakeSureServerIsRunning()

	// lets attempt to login to the Mattermost server as the bot user
	// This will set the token required for all future calls
	// You can get this token with client.AuthToken
	LoginAsTheBotUser()

	// If the bot user doesn't have the correct information lets update his profile
	UpdateTheBotUserIfNeeded()

	// Lets find our bot team
	FindBotTeam()
	// This is an important step.  Lets make sure we use the botTeam
	// for all future web service requests that require a team.
	//client.SetTeamId(botTeam.Id)

	// Lets create a bot channel for logging debug messages into
	CreateBotDebuggingChannelIfNeeded()
	SendMsgToDebuggingChannel("_"+config.LongName+" has **started** running_", "")

	actions = []Action{
		Action{Name: "Debug Log Channel Handler", Event: model.WEBSOCKET_EVENT_POSTED, Handler: HandleMsgFromDebuggingChannel},
		Action{Name: "Command Handler", Event: model.WEBSOCKET_EVENT_POSTED, Handler: HandleCommands},

		Action{Name: "HandleShowAllChannelEvents", Handler: HandleShowAllChannelEvents},
		Action{Name: "HandleJoins", Event: model.WEBSOCKET_EVENT_USER_ADDED, Handler: HandleJoins},
	}

	commands = []Command{
		Command{Name: "help",
			Description: "print out this help text",
			Handler: func(event *model.WebSocketEvent, post *model.Post) error {
				var cmdList []string
				for _, cmd := range commands {
					cmdList = append(cmdList, cmd.Name+": "+cmd.Description)
				}
				helpText := "Commands available:\n"
				helpText += strings.Join(cmdList, "\n")
				SendMsgToChannel(event.Broadcast.ChannelId, helpText, post.Id)
				return nil
			},
		},
	}
	// Lets start listening to some channels via the websocket!
	webSocketClient, apperr := model.NewWebSocketClient4("wss://"+config.Domain, client.AuthToken)
	if err != nil {
		println("We failed to connect to the web socket")
		PrintError(apperr)
	}

	webSocketClient.Listen()

	go func() {
		for {
			select {
			case resp := <-webSocketClient.EventChannel:
				HandleWebSocketResponse(resp)
			}
		}
	}()

	// You can block forever with
	select {}
}

func MakeSureServerIsRunning() {
	if props, resp := client.GetOldClientConfig(""); resp.Error != nil {
		println("There was a problem pinging the Mattermost server.  Are you sure it's running?")
		PrintError(resp.Error)
		os.Exit(1)
	} else {
		println("Server detected and is running version " + props["Version"])
	}
}

func LoginAsTheBotUser() {
	if user, resp := client.Login(config.UserEmail, config.UserPassword); resp.Error != nil {
		println("There was a problem logging into the Mattermost server.  Are you sure ran the setup steps from the README.md?")
		PrintError(resp.Error)
		os.Exit(1)
	} else {
		botUser = user
	}
}

func UpdateTheBotUserIfNeeded() {
	if botUser.FirstName != config.UserFirst || botUser.LastName != config.UserLast || botUser.Username != config.UserName {
		botUser.FirstName = config.UserFirst
		botUser.LastName = config.UserLast
		botUser.Username = config.UserName

		if user, resp := client.UpdateUser(botUser); resp.Error != nil {
			println("We failed to update the Sample Bot user")
			PrintError(resp.Error)
			os.Exit(1)
		} else {
			botUser = user
			println("Looks like this might be the first run so we've updated the bots account settings")
		}
	}
}

func FindBotTeam() {
	if team, resp := client.GetTeamByName(config.TeamName, ""); resp.Error != nil {
		println("We failed to get the initial load")
		println("or we do not appear to be a member of the team '" + config.TeamName + "'")
		PrintError(resp.Error)
		os.Exit(1)
	} else {
		botTeam = team
	}
}

func CreateBotDebuggingChannelIfNeeded() {
	if rchannel, resp := client.GetChannelByName(config.LogChannel, botTeam.Id, ""); resp.Error != nil {
		println("We failed to get the channels")
		PrintError(resp.Error)
	} else {
		debuggingChannel = rchannel
		return
	}

	// Looks like we need to create the logging channel
	channel := &model.Channel{}
	channel.Name = config.LogChannel
	channel.DisplayName = "Debugging For Sample Bot"
	channel.Purpose = "This is used as a test channel for logging bot debug messages"
	channel.Type = model.CHANNEL_OPEN
	channel.TeamId = botTeam.Id
	if rchannel, resp := client.CreateChannel(channel); resp.Error != nil {
		println("We failed to create the channel " + config.LogChannel)
		PrintError(resp.Error)
	} else {
		debuggingChannel = rchannel
		println("Looks like this might be the first run so we've created the channel " + config.LogChannel)
	}
}

func SendMsgToDebuggingChannel(msg string, replyToId string) {
	SendMsgToChannel(debuggingChannel.Id, msg, replyToId)
}

func SendMsgToChannel(channel string, msg string, replyToId string) {
	post := &model.Post{}
	post.ChannelId = channel
	post.Message = msg

	post.RootId = replyToId

	if _, resp := client.CreatePost(post); resp.Error != nil {
		println("We failed to send a message to the logging channel")
		PrintError(resp.Error)
	}
}

func SendDirectMessage(id string, msg string) {
	result, err := client.CreateDirectChannel(id, botUser.Id)
	if result == nil {
		fmt.Printf("ERROR:  %v\n", err)
		return
	}
	fmt.Printf("result is : %v\n", result)

	post := &model.Post{}
	post.Message = msg
	post.ChannelId = result.Id
	if _, resp := client.CreatePost(post); resp.Error != nil {
		println("We failed to send a message to the direct channel")
		PrintError(resp.Error)
	}
}

func HandleWebSocketResponse(event *model.WebSocketEvent) {
	for _, a := range actions {

		// if event filter is set then skip this event if it doesn't match
		if a.Event != "" && event.Event != a.Event {
			continue
		}
		err := a.Handler(event)
		if err != nil {
			fmt.Printf("error running action %s:%v\n", a.Name, err)
		}
	}
}

//  Handlers ----------------------------------------------

func HandleJoins(event *model.WebSocketEvent) (err error) {
	if event.Broadcast.ChannelId != debuggingChannel.Id {
		return
	}

	SendDirectMessage(event.Data["user_id"].(string), "you just got added to the channel!")

	return
}

func HandleShowAllChannelEvents(event *model.WebSocketEvent) (err error) {
	if event.Broadcast.ChannelId != debuggingChannel.Id {
		return
	}
	if event.Event == model.WEBSOCKET_EVENT_POSTED {
		return
	}

	SendMsgToDebuggingChannel(fmt.Sprintf("I just got this event:%v with data: %v ", event.Event, event.Data), "")
	return
}

const (
	HelpString = "commands: help,fish"
)

func HandleCommands(event *model.WebSocketEvent) (err error) {
	// If this isn't the debugging channel then lets ingore it
	//	if event.Broadcast.ChannelId != debuggingChannel.Id {
	//		return
	//	}

	println("responding to debugging channel msg")
	post := model.PostFromJson(strings.NewReader(event.Data["post"].(string)))
	if post != nil {
		// ignore my events
		if post.UserId == botUser.Id {
			return
		}

		// ignore anything that doesn't say @bot
		if matched, _ := regexp.MatchString(`(?:^|\W)@`+config.UserName+`(?:$|\W)`, post.Message); matched {

			for _, cmd := range commands {
				// help command
				if matched, _ := regexp.MatchString(`(?:^|\W)`+cmd.Name+`(?:$|\W)`, post.Message); matched {
					cmd.Handler(event, post)
				}
			}

			return
		}

	}
	return
}
func HandleMsgFromDebuggingChannel(event *model.WebSocketEvent) (err error) {
	// If this isn't the debugging channel then lets ingore it
	if event.Broadcast.ChannelId != debuggingChannel.Id {
		return
	}

	println("responding to debugging channel msg")

	post := model.PostFromJson(strings.NewReader(event.Data["post"].(string)))
	if post != nil {

		// ignore my events
		if post.UserId == botUser.Id {
			return
		}

		// if you see any word matching 'alive' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)alive(?:$|\W)`, post.Message); matched {
			SendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}

		// if you see any word matching 'up' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)up(?:$|\W)`, post.Message); matched {
			SendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}

		// if you see any word matching 'running' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)running(?:$|\W)`, post.Message); matched {
			SendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}

		// if you see any word matching 'hello' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)hello(?:$|\W)`, post.Message); matched {
			SendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}
	}

	SendMsgToDebuggingChannel("I did not understand you!", post.Id)
	return
}

func PrintError(err *model.AppError) {
	println("\tError Details:")
	println("\t\t" + err.Message)
	println("\t\t" + err.Id)
	println("\t\t" + err.DetailedError)
}

func SetupGracefulShutdown() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for _ = range c {
			if webSocketClient != nil {
				webSocketClient.Close()
			}

			SendMsgToDebuggingChannel("_"+config.LongName+" has **stopped** running_", "")
			os.Exit(0)
		}
	}()
}
