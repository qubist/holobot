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
	"time"
	"errors"
)

type Config struct {
	LongName     string
	UserName     string
	UserEmail    string
	UserFirst    string
	UserLast     string
	UserPassword string
	TeamName     string
	PublicTeam   bool
	LogChannel   string
	Domain       string
	Debugging    bool
}

var config Config
var client *model.Client4
var webSocketClient *model.WebSocketClient

var botUser *model.User
var botTeam *model.Team
var debuggingChannel *model.Channel
var townsquareChannel *model.Channel
var announcementsChannel *model.Channel

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

	// Let's test to see if the mattermost server is up and running
	MakeSureServerIsRunning()

	// let's attempt to login to the Mattermost server as the bot user
	// This will set the token required for all future calls
	// You can get this token with client.AuthToken
	LoginAsTheBotUser()

	// If the bot user doesn't have the correct information let's update its profile
	UpdateTheBotUserIfNeeded()

	// Let's find our teams
	botTeam = FindTeam(config.TeamName)

	// This is an important step.  Let's make sure we use the botTeam
	// for all future web service requests that require a team.
	//client.SetTeamId(botTeam.Id)

	GetAnnouncements()
	GetTownSquare()

	//array of all the actions
	actions = []Action{
		Action{Name: "Command Handler", Event: model.WEBSOCKET_EVENT_POSTED, Handler: HandleCommands},
		Action{Name: "About DM Response", Event: model.WEBSOCKET_EVENT_POSTED, Handler: HandleDMs},
	}
	// if debug mode is on, activate the Debug Log Channel Handler, and do some other things
	fmt.Printf("imported config.yaml data:\n%v\n", config)
	if config.Debugging {
		println("DEGUBBING IS ON, BOIS")
		actions = append(actions, Action{Name: "Debug Log Channel Handler",
															Event: model.WEBSOCKET_EVENT_POSTED,
															Handler: HandleMsgFromDebuggingChannel})
		actions = append(actions, Action{Name: "HandleShowAllChannelEvents",
															Handler: HandleShowAllChannelEvents})

		// Let's create a bot channel for logging debug messages into
		CreateBotDebuggingChannelIfNeeded()
		SendMsgToDebuggingChannel("_"+config.LongName+" has **started** running_", "")
	}

	// add public team actions only if running in the public team
	if config.PublicTeam {
		actions = append(actions, Action{Name: "Delete \"Joined\" Alerts",
															Event: model.WEBSOCKET_EVENT_POSTED,
															Handler: HandleAnnouncementJoinMessages})
		actions = append(actions, Action{Name: "Welcome Actions - Msg, Add to Announce., etc",
															Event: model.WEBSOCKET_EVENT_NEW_USER,
															Handler: HandleTeamJoins})
	}

	commands = []Command{
		/*Command{
			Name: "help",
			Description: "Print out this help text.",
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
		}, */

		//time command
		Command{
			Name: "time",
			Description: "Displays times mentioned in the message in various relevant time zones.",
			Handler: func(event *model.WebSocketEvent, post *model.Post) error {
				//regex to match valid times with time zones (ex. "1 GMT", "2:00 AM EST", "15:00 PT", etc.)
				re := regexp.MustCompile(`([0-9]+)(:[0-9]+)* *([paPA][mM])* *([a-zA-Z]+)((\+|\-)([0-9]+))*`)
				if matches := re.FindAllStringSubmatch(post.Message, -1); matches != nil {
					for _, m := range matches {
						layout := "15"
						input := m[1]
						if len(m) > 2 && m[2] != "" {
							layout += ":04"
							input+=m[2]
						}
						if len(m) > 3 && m[3] != "" {
							layout += "PM"
							input+=m[3]
						}

						// determine location from input
						var err error
						var loc string
						// recognized time zones strings
						switch strings.ToUpper(m[4]) {
						case "PST", "PT", "PACIFIC":
							loc = "America/Los_Angeles"
						case "MST", "MT", "MOUNTAIN":
							loc = "America/Denver"
						case "CST", "CT", "CENTRAL":
							loc = "America/Chicago"
						case "EST", "ET", "EASTERN":
							loc = "America/New_York"
						case "GMT", "UTC", "GREENWICH":
							loc = "etc/UTC"
						case "CHINA", "SHANGHAI", "BEIJING":
							loc = "China/Shanghai"
						case "ECT", "QUITO", "ECUADOR":
							loc = "America/Guayaquil"
						case "IST", "INDIAN", "INDIA":
							loc = "Asia/Kolkata"
						default:
							loc = m[4] //default
						}
						if m[5]!="" { // if there's a plus or minus on the time zone,
							if strings.ToUpper(m[4]) == "GMT" { // and it's GMT,
								loc = "Etc/GMT"// set location to GMT plus whatever was in the input
								if m[6]=="+"{
									loc+="-"+m[7]
								} else if m[6]=="-"{
									loc+="+"+m[7]
								}
							} else { // if it's not GMT,
								err = errors.New("") // throw an error.
							}
						}

						var t time.Time
						var l *time.Location

						// parses the time in whichever location was specified (golang time library magic)
						if err == nil {
							l, err = time.LoadLocation(loc)
							if err == nil {
								t, err = time.ParseInLocation(layout, strings.ToUpper(input), l)
							}
						}

						var timeZoneText string
						var debuggingTimeZoneText string

						// converts time into the desired output time zones,
						if err != nil {
							timeZoneText = fmt.Sprintf("I couldn't understand the time: \"%s\"", m[0])
						}else{
							ptl,_:=time.LoadLocation("America/Los_Angeles")
							pt := t.In(ptl).Format("3:04 PM")
							mtl,_:=time.LoadLocation("MST")
							mt := t.In(mtl).Format("3:04 PM")
							ctl,_:=time.LoadLocation("America/Chicago")
							ct := t.In(ctl).Format("3:04 PM")
							etl,_:=time.LoadLocation("EST")
							et := t.In(etl).Format("3:04 PM")
							gmtl,_:=time.LoadLocation("GMT")
							gmt := t.In(gmtl).Format("15:04")
							cetl,_:=time.LoadLocation("CET")
							cet := t.In(cetl).Format("15:04")
							// and prints them in a table
							timeZoneText = fmt.Sprintf(`"%s" is:

|   PT    |   MT   |   CT   |   ET   |  GMT   |  CET   |
|:-------:|:------:|:------:|:------:|:------:|:------:|
|   %s    |   %s   |   %s   |   %s   |   %s   |   %s   |`, m[0], pt, mt, ct, et, gmt, cet)

							// make a debugging message with extra info about the above processes
							debuggingTimeZoneText = fmt.Sprintf("➚ *Debugging Info:*\n(%v)\nTime zone I heard (m[4]) was: %v\nLocation (l): %v", t, m[4], l)
						}
						SendMsgToChannel(event.Broadcast.ChannelId, timeZoneText, post.Id)
						// send debugging message if debugging is turned on
						if config.Debugging {
							SendMsgToChannel(event.Broadcast.ChannelId, debuggingTimeZoneText, post.Id)
						}

					}


				}
				return nil
			},
		},
	}
	// Let's start listening to some channels via the websocket!
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
		println("There was a problem logging into the Mattermost server. Are you sure ran the setup steps from the README.md?")
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

func FindTeam(name string) *model.Team {
	team, resp := client.GetTeamByName(name, "")
	if resp.Error != nil {
		println("We failed to get the initial load")
		println("or we do not appear to be a member of the team '" + name + "'")
		PrintError(resp.Error)
		os.Exit(1)
	}
	return team
}

func GetAnnouncements() {
	if rchannel, resp := client.GetChannelByName("announcements", botTeam.Id, ""); resp.Error != nil {
		println("We failed to get the annoucements channel")
		PrintError(resp.Error)
	} else {
		announcementsChannel = rchannel
		if config.Debugging {
			fmt.Println("Announcements channel gotten as: ", rchannel)
		}
	}
}

func GetTownSquare() {
	if rchannel, resp := client.GetChannelByName("town-square", botTeam.Id, ""); resp.Error != nil {
		println("We failed to get the town-square channel")
		PrintError(resp.Error)
	} else {
		townsquareChannel = rchannel
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

func HandleAnnouncementJoinMessages(event *model.WebSocketEvent)	 (err error) {
	// don't do anything if the channel that was joined was not Announcements
	if event.Broadcast.ChannelId != announcementsChannel.Id {
		return
	}
	// if debugging is on, print some messages
	if config.Debugging {
		fmt.Println("Looks like someone just joined announcements: %v", event.Data )
		SendMsgToDebuggingChannel("Hey! Someone just joined `announcements`!", "")
	}
	// do the actual deleting
	post := model.PostFromJson(strings.NewReader(event.Data["post"].(string)))
	sender := event.Data["sender_name"].(string)
	if (sender + " has joined the channel." == post.Message) || (sender + " has left the channel." == post.Message) {
		client.DeletePost(post.Id)
		SendMsgToDebuggingChannel(fmt.Sprintf("Deleted this post: %v", post.Message), "")
	}
 return
}

func HandleTeamJoins(event *model.WebSocketEvent) (err error) {
	user := event.Data["user_id"].(string)

	teams, _ := client.GetTeamsForUser(user, "")
	if teams != nil && len(teams) == 1 {
		if teams[0].Id == botTeam.Id { // if there's a brand new user and on the public team...
			// spin off go routine to wait a bit before sending a direct message
			go func() {
				time.Sleep(time.Second * 7)
				SendDirectMessage(user, `Welcome! I'm **holobot**. I'll help you get started around here.

See those **Public Channels** in the menu on the left? That's where most everything happens around here. Once you're in a channel you can click on the header to get more information about the channel, and how it operates. If you see "?: @someonesname" that means that @someonesname is the **Steward** of that channel. Let the Steward know if you have any questions, or need direction.

I've automatically added you to the **~announcements** channel! This is a low-volume channel for brief, relevant announcements. Posts that aren't announcements in that channel get deleted, so watch out for that. (If you need to respond to an announcement, post in **~town-square** and link back to the announcement.)

Feel free to introduce yourself to everybody in **~town-square,** and click on "More..."  to join all the channels that interest you!

See you around :)`)
			}()
			// and add the user to announcements
			client.AddChannelMember(announcementsChannel.Id, user)
		}
	} else {
		fmt.Printf("A new user is somehow in no team or more than one team‽‽ That's preposterous!")
	}
	return
}

func HandleDMs(event *model.WebSocketEvent) (err error) {
	name := event.Data["channel_name"].(string)
	if matched, _ := regexp.MatchString(`(^`+botUser.Id+`__)|(__`+botUser.Id+`$)` , name); matched {
		post := model.PostFromJson(strings.NewReader(event.Data["post"].(string)))
		if matched, _ := regexp.MatchString(`(?:^|\W)help(?:$|\W)`, post.Message); matched {
			SendDirectMessage(post.UserId, `Hi, I'm holobot! I automatically perform various actions to help things run smoother around the team. I can also help you out with commands!

| Command | Description | Usage |
|---------|-----------------------------------------------------------------------------------------------------------------|----------------------------------------|
| time | I'll reply with a handy chart translating times you mentioned in your message into various relevant time zones. | *"Meeting at 9 AM EST? @holobot time"* |

If you have questions, feedback, or suggestions, send @will a direct message.

:)`)
		}
	}
	return
}

func HandleShowAllChannelEvents(event *model.WebSocketEvent) (err error) {
	// if event.Broadcast.ChannelId != debuggingChannel.Id {
	// 	return
	// }
	if event.Event == model.WEBSOCKET_EVENT_POSTED || event.Event == model.WEBSOCKET_EVENT_CHANNEL_VIEWED {
		fmt.Printf("I just got this event: \"%v\" with data: \"%v\"\n\n\n", event.Event, event.Data)
		return
	}
	SendMsgToDebuggingChannel(fmt.Sprintf("I just got this event: \"%v\" with data: \"%v\"", event.Event, event.Data), "")
	return
}

func HandleCommands(event *model.WebSocketEvent) (err error) {
	// If this isn't the debugging channel then let's ingore it
		// if event.Broadcast.ChannelId != debuggingChannel.Id {
		// 	return
		// }

	println("checking for commands via HandleCommands")
	post := model.PostFromJson(strings.NewReader(event.Data["post"].(string)))
	if post != nil {
		// ignore my events
		if post.UserId == botUser.Id {
			return
		}

		// ignore anything that doesn't say @holobot
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
	// if debugging mode is on...
	if config.Debugging {
		// If this isn't the debugging channel then let's ingore it
		if event.Broadcast.ChannelId != debuggingChannel.Id {
			return
		}

		println("attempting response to debugging channel msg via `HandleMsgFromDebuggingChannel`")

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

		return
	}else {
	return
	}
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
			if config.Debugging {
				SendMsgToDebuggingChannel("_"+config.LongName+" has **stopped** running_", "")
			}
			os.Exit(0)
		}
	}()
}
