// Copyright (c) 2016 Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package main

import (
	"errors"
	"fmt"
	"github.com/mattermost/mattermost-server/model"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"
)

type Config struct {
	LongName          string
	UserName          string
	UserEmail         string
	UserFirst         string
	UserLast          string
	UserPassword      string
	PublicTeamName    string
	PrivateTeamName   string
	DebuggingTeamName string
	LogChannel        string
	Domain            string
	Debugging         bool
}

var config Config
var client *model.Client4
var webSocketClient *model.WebSocketClient

var botUser *model.User
var publicTeam, privateTeam, debuggingTeam *model.Team
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
	fn := os.Getenv("HOLOBOT_CONFIG")
	if fn == "" {
		fn = "config.yaml"
	}

	f, err := os.Open(fn)
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

	// client = model.NewAPIv4Client("http://" + config.Domain) //FOR TESTING
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
	publicTeam = FindTeam(config.PublicTeamName)
	privateTeam = FindTeam(config.PrivateTeamName)
	debuggingTeam = FindTeam(config.DebuggingTeamName)

	announcementsChannel = FindChannel("announcements", publicTeam)
	if announcementsChannel == nil {
		return
	}

	//array of all the actions
	actions = []Action{
		Action{Name: "Command Handler", Event: model.WEBSOCKET_EVENT_POSTED, Handler: HandleCommands},
		Action{Name: "About DM Response", Event: model.WEBSOCKET_EVENT_POSTED, Handler: HandleDMs},
		Action{Name: "Delete \"Joined\" Alerts", Event: model.WEBSOCKET_EVENT_POSTED, Handler: HandleAnnouncementMessages},
		Action{Name: "Welcome Actions—Msg, Add to Announce., etc", Event: model.WEBSOCKET_EVENT_NEW_USER, Handler: HandleTeamJoins},
		Action{Name: "Delete Own Message", Event: model.WEBSOCKET_EVENT_REACTION_ADDED, Handler: HandleReactions},
	}
	// if debug mode is on, activate the Debug Log Channel Handler, and do some other things
	//fmt.Printf("imported config.yaml data:\n%v\n", config)
	if config.Debugging {
		println("DEGUBBING IS ON, BOIS")
		actions = append(actions, Action{Name: "Debug Log Channel Handler",
			Event:   model.WEBSOCKET_EVENT_POSTED,
			Handler: HandleMsgFromDebuggingChannel})
		actions = append(actions, Action{Name: "HandleShowAllChannelEvents",
			Handler: HandleShowAllChannelEvents})

		// Let's create a bot channel for logging debug messages into
		CreateBotDebuggingChannelIfNeeded()
		SendMsgToChannel(debuggingChannel.Id, "_"+config.LongName+" has **started** running_", "")
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

		// time command
		Command{
			Name:        "time",
			Description: "Displays times mentioned in the message in various relevant time zones.",
			Handler: func(event *model.WebSocketEvent, post *model.Post) error {
				// regex to match valid times with time zones (ex. "1 GMT", "2:00 AM EST", "15:00 PT", etc.)
				re := regexp.MustCompile(`([0-9]{1,2})(:[0-9]{1,2})? *([paPA]\.?[mM]\.?)? +([A-Za-z][a-zA-Z]+)((\+|\-)([0-9]{1,2})(?:\s|\W|$))?`) // big ol' hairy regex
				if matches := re.FindAllStringSubmatch(post.Message, -1); matches != nil {
					for _, m := range matches {
						layout := "15"
						input := m[1]
						if len(m) > 2 && m[2] != "" {
							layout += ":04"
							input += m[2]
						}
						if len(m) > 3 && m[3] != "" {
							layout += "PM"
							input += m[3]
							if len(m[3]) == 1 {
								input += "M"
							}
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
						case "EST", "ET", "EASTERN", "EAST":
							loc = "America/New_York"
						case "GMT", "UTC", "GREENWICH":
							loc = "Etc/UTC"
						case "CHINA", "CHINESE", "SHANGHAI", "BEIJING":
							loc = "Asia/Shanghai"
						case "ECT", "QUITO", "ECUADOR", "ECUADORIAN":
							loc = "America/Guayaquil"
						case "IST", "INDIAN", "INDIA":
							loc = "Asia/Kolkata"
						default:
							loc = m[4] //default
						}
						if m[5] != "" { // if there's a plus or minus on the time zone,
							if strings.ToUpper(m[4]) == "GMT" { // if timezone is GMT,
								loc = "Etc/GMT" // set location to GMT plus whatever was in the input
								if m[6] == "+" {
									loc += "-" + m[7]
								} else if m[6] == "-" {
									loc += "+" + m[7]
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
								// gotta give it today's date so it works correctly
								now := time.Now()
								date := now.Format("01/02/2006 ")
								t, err = time.ParseInLocation("01/02/2006 "+layout, date+strings.ToUpper(input), l)
								if err != nil {
									fmt.Printf("error parsing time: %v\n", err)
								}
							} else {
								fmt.Printf("Error loading location %s: %v\n", loc, err)
							}
						}

						var timeZoneText string
						var debuggingTimeZoneText string

						// converts time into the desired output time zones,
						if err != nil {
							timeZoneText = fmt.Sprintf("I couldn't understand the time: \"%s\"", m[0])
						} else {
							ptl, _ := time.LoadLocation("America/Los_Angeles")
							pt := t.In(ptl).Format("3:04 PM")
							mtl, _ := time.LoadLocation("MST")
							mt := t.In(mtl).Format("3:04 PM")
							ctl, _ := time.LoadLocation("America/Chicago")
							ct := t.In(ctl).Format("3:04 PM")
							etl, _ := time.LoadLocation("EST")
							et := t.In(etl).Format("3:04 PM")
							gmtl, _ := time.LoadLocation("GMT")
							gmt := t.In(gmtl).Format("15:04")
							cetl, _ := time.LoadLocation("CET")
							cet := t.In(cetl).Format("15:04")
							istl, _ := time.LoadLocation("Asia/Kolkata")
							ist := t.In(istl).Format("3:04 PM")
							// and prints them in a table
							timeZoneText = fmt.Sprintf(`"%s" is:

|   PT    |   MT   |   CT   |   ET   |  GMT   |  CET   |  IST   |
|:-------:|:------:|:------:|:------:|:------:|:------:|:------:|
|   %s    |   %s   |   %s   |   %s   |   %s   |   %s   |   %s   |`, m[0], pt, mt, ct, et, gmt, cet, ist)

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
	// webSocketClient, apperr := model.NewWebSocketClient4("ws://"+config.Domain, client.AuthToken) //FOR TESTING
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

func FindChannel(name string, team *model.Team) *model.Channel {
	rchannel, resp := client.GetChannelByName(name, team.Id, "")
	if resp.Error != nil {
		fmt.Printf("We failed to get the %v channel", name)
		PrintError(resp.Error)
	} else {
		if config.Debugging {
			fmt.Printf("%v channel gotten as: %v", name, rchannel)
		}
	}
	return rchannel
}

func CreateBotDebuggingChannelIfNeeded() {
	debuggingChannel = FindChannel(config.LogChannel, debuggingTeam)
	if debuggingChannel != nil {
		return
	}
	// Looks like we need to create the logging channel
	channel := &model.Channel{}
	channel.Name = config.LogChannel
	channel.DisplayName = "Debugging For Sample Bot"
	channel.Purpose = "This is used as a test channel for logging bot debug messages"
	channel.Type = model.CHANNEL_OPEN
	channel.TeamId = debuggingTeam.Id
	if rchannel, resp := client.CreateChannel(channel); resp.Error != nil {
		println("We failed to create the channel " + config.LogChannel)
		PrintError(resp.Error)
	} else {
		debuggingChannel = rchannel
		println("Looks like this might be the first run so we've created the channel " + config.LogChannel)
	}
}

func SendMsgToDebuggingChannel(msg string, replyToId string) {
	if config.Debugging {
		SendMsgToChannel(debuggingChannel.Id, msg, replyToId)
	}
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
	if id != botUser.Id {
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
	} else if id == botUser.Id {
		SendMsgToDebuggingChannel(fmt.Sprintf("**Prevented holobot from DMing itself this message:**\n\n```\n\n%v\n\n```", msg), "")
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

func HandleAnnouncementMessages(event *model.WebSocketEvent) (err error) {
	// don't do anything if the channel that was joined was not Announcements
	if event.Broadcast.ChannelId != announcementsChannel.Id {
		return
	}
	// if debugging is on, print some messages
	if config.Debugging {
		fmt.Println("Looks like someone just joined announcements: %v", event.Data)
		SendMsgToDebuggingChannel("Hey! Someone just joined `announcements`!", "")
	}
	// do the actual deleting
	post := model.PostFromJson(strings.NewReader(event.Data["post"].(string)))
	sender := event.Data["sender_name"].(string)
	SendMsgToDebuggingChannel(fmt.Sprintf("Checking to see if this post is a join or leave message: %v\nSender: %v\nconfig.UserName: %v", post.Message, sender, config.UserName), "")
	//if the newest message is a join message, and leave message, or an added message, delete it.
	if matched, _ := regexp.MatchString(`(?:^|\W)((`+sender+` has (joined|left) the channel\.)|(.+ (added to|removed from) the channel by `+config.UserName+`))(?:$)`, post.Message); matched {
		// if (sender + " has joined the channel." == post.Message) || (sender + " has left the channel." == post.Message) {
		client.DeletePost(post.Id)
		SendMsgToDebuggingChannel(fmt.Sprintf("Deleted this post: %v", post.Message), "")
	}
	return
}

func HandleTeamJoins(event *model.WebSocketEvent) (err error) {
	user := event.Data["user_id"].(string)
	go func() { // spin off go routine to wait a bit before welcoming them
		time.Sleep(time.Second * 55)
		teams, _ := client.GetTeamsForUser(user, "")
		if config.Debugging {
			fmt.Printf("teams: %v\npublicTeam: %v\n", teams, publicTeam)
		}
		if teams != nil && len(teams) == 1 {
			println("USER IS IN EXACTLY ONE TEAM! (yuss)\n")
			if teams[0].Id == publicTeam.Id { // if the user is brand new user, and only on the public team...
				fmt.Printf("USER's ONE TEAM IS: %v\n", publicTeam)
				// send them the welcome text as a direct message:
				SendDirectMessage(user,
					"# Welcome! "+"\n"+
						"I'm **holobot**! I'll help you get started around here. Here's some useful info:"+"\n"+
						"##### Channels and Stewards"+"\n"+
						"See those **Public Channels** in the menu on the left? That's where most everything happens around here. Once you're in a channel you can click on the header to see information about the channel's purpose, and how it operates. The users after the `?:` in the channel header are the the **Stewards** for that channel. Let the Steward know if you have any questions, or need direction."+"\n"+
						"##### The Announcements Channel"+"\n"+
						"I've automatically added you to the **~announcements** channel! This is a low-volume channel for brief, relevant announcements. Posts that aren't announcements in that channel get deleted, so watch out for that. (If you need to respond to an announcement, post in **~town-square** and either link back to the announcement, or quote it by prepending it with `> `.)"+"\n"+
						"##### Q&A Channels"+"\n"+
						"Channels beginning with `❓`—like ~holo-currency-qa, ~holochain-tech-qa, and ~holoport-host-qa—are specially designated Q&A channels. If you've got a question, check to see if the relevant Q&A channel answers it, and then go ahead and post there."+"\n"+
						"##### Other Channels"+"\n"+
						"The ~app-ideas channel is a great place to post possible applications of the Holochain technology and brainstorm how potential apps would look, work, and feel. The ~app-dev channel is good for discussing Holochain applications in active states of development."+"\n"+
						"##### A Few Mattermost Tips"+"\n"+
						"* Click the tiny reply arrow icon on a post to **reply** directly to it. This pulls up the thread in the pane on the right. Replies show up in the channel as new posts replying to older posts, with a backlink you can click to pull up the entire thread for easy review."+"\n"+
						"* Click the star next to a channel's title to **favorite** it. Favorited channels appear at the top of your list."+"\n"+
						"* Press Ctrl-K/Cmd-K to open a **search box** to type and quickly jump to a channel."+"\n"+
						"You can direct message me `mattermost tips` to see more."+"\n"+
						"***"+"\n"+
						"It's good to have you here! Feel free to introduce yourself to everybody in **~town-square,** and click on `More...` to join all the channels that interest you!"+"\n"+
						"See you around :)")
				// and add the user to announcements
				client.AddChannelMember(announcementsChannel.Id, user)
			}
		} else {
			fmt.Printf("A new user is somehow in no team or more than one team‽‽ That's preposterous!\n")
			fmt.Printf("Teams data: %v\n", teams)
		}
	}()
	return
}

func HandleDMs(event *model.WebSocketEvent) (err error) {
	name := event.Data["channel_name"].(string)
	// if the new post is in a DM channel to the bot
	if matched, _ := regexp.MatchString(`(^`+botUser.Id+`__)|(__`+botUser.Id+`$)`, name); matched {
		post := model.PostFromJson(strings.NewReader(event.Data["post"].(string)))
		// if the message contains the string "help", "halp", or a variotion of "who are you?"
		if matched, _ := regexp.MatchString(`(?i)(?:^|\W)help|halp|who are you(?:$|\W)`, post.Message); matched {
			SendDirectMessage(post.UserId,
				"Hi, I'm holobot! I cheerfully and automatically perform various actions to help things run smoother around the team. I can also help you out with commands!"+"\n"+"\n"+
					"Use a command by typing `@holobot` followed by the command's name. For example, typing `@holobot time` will execute my \"time\" command."+"\n"+"\n"+
					"Note: I'm only able to execute commands in channels I'm a part of, and in direct messages with me. You can add me to your channel by clicking on the channel header and then on `Add Members`. I cant read your direct messages."+"\n"+"\n"+
					// I'm using this ridiculous number of non-breaking spaces as a hacky (read: very very hacky) way of making the usage exapmles not wrap at the space inbetween "@holobot" and the command (ex. "time")
					"| Command | Description |    Usage&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;  | Example |"+"\n"+
					"|---------|-------------|---|---|"+"\n"+
					"| `time`  | I'll reply with a handy table translating the times you mentioned in your message into various relevant time zones. | `@holobot time` | *Does a meeting at 9 AM EST work for everyone? @holobot time* |"+"\n"+"\n"+
					"If you have questions, feedback, or suggestions, send @will a direct message. :)")
		}
		// if the message contains the string "mattermost tips"
		if matched, _ := regexp.MatchString(`(?i)(?:^|\W)mattermost tips(?:$|\W)`, post.Message); matched {
			SendDirectMessage(post.UserId, //send tips
				"##### Mattermost Tips"+"\n"+
					"* Click the tiny reply arrow icon on a post to **reply** directly to it. This pulls up the thread in the pane on the right. Replies show up in the channel as new posts replying to older posts, with a backlink you can click to pull up the entire thread for easy review."+"\n"+
					"* Click the star next to a channel's title to **favorite** it. Favorited channels show up in a list at the top, so this is the best way to stay plugged-in to the key channels you are involved with. You can favorite both public and private channels as well as private conversations."+"\n"+
					"* Press Ctrl-K/Cmd-K to open a **search box** to type and quickly jump to a channel."+"\n"+
					"* Click the flag next to the timestamp on a message to **flag** it. The list of posts you have flagged can be seen by clicking the flag icon in the top right corner of the screen. Use flags to keep track of posts for follow-up, or to save them for later. It's a great replacement for \"Mark as Unread\"!"+"\n"+
					"* Click the `@` icon next to the flag icon to see a list of **mentions** of you. You can change what will trigger a mention in Account Settings > Notifications."+"\n"+
					"* Use emojis to **react** to posts without triggering a notification or making people read more text. Reactions are also sometimes used for voting or polling."+"\n"+
					"* **Channel headers** can list links to core founding documents and key locations for each channel."+"\n"+
					"* Click a user's **profile picture** to see their info or send them a direct message."+"\n"+
					"* **Mention** someone with `@username`. `@username` will always trigger a mention for them. Using someone's first name can also trigger a mention, depending on their settings."+"\n"+
					"* `@channel` and `@all` trigger **channel-wide mentions** that notify everyone in the channel. Use these sparingly and in the most specific relevant channel to avoid triggering mentions for unrelated people."+"\n"+
					"* You can use specific rules to render messages with special **formatting.** Check [Mattermost's formatting guide](https://docs.mattermost.com/help/messaging/formatting-text.html) for detailed documentation of all these rules."+"\n"+
					"* **Pin** posts that are announcements or have long-term value for a channel. To pin a post, mouse over the post, then click the tiny `[...]` icon which appears to access the menu, then click `Pin to channel`. To view all the pinned posts in a channel, click the thumbtack icon to the left of the search bar.")
		}
	}
	return
}

func HandleReactions(event *model.WebSocketEvent) (err error) {
	//fmt.Printf("Event data: %v\n\n", event.Data)
	reaction := model.ReactionFromJson(strings.NewReader(event.Data["reaction"].(string)))
	post, _ := client.GetPost(reaction.PostId, "")
	// Debugging I was doing to find out what information I needed and how to get it:
	// SendMsgToDebuggingChannel(fmt.Sprintf("Emoji name: %v\nPost Id: %v\nPost: %v", reaction.EmojiName, reaction.PostId, post), "")

	// Check if the post was made by holobot
	if post.UserId == botUser.Id {
		SendMsgToDebuggingChannel("Reaction to holobot detected!!", "")
		// If it was, check if the reaction was :x:
		if reaction.EmojiName == "x" {
			// If it was, delete the post
			client.DeletePost(post.Id)
			if config.Debugging {
				fmt.Printf("Deleted this post due to \"x\" reaction: %v\n", post)
			}
		}
	}
	return
}

func HandleShowAllChannelEvents(event *model.WebSocketEvent) (err error) {
	// if event.Broadcast.ChannelId != debuggingChannel.Id {
	// 	return
	// }
	if event.Event == model.WEBSOCKET_EVENT_POSTED || event.Event == model.WEBSOCKET_EVENT_CHANNEL_VIEWED || event.Event == model.WEBSOCKET_EVENT_TYPING {
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
	if config.Debugging {
		println("checking for commands via HandleCommands")
	}
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
				if matched, _ := regexp.MatchString(`(?:^|\W)@`+config.UserName+` `+cmd.Name+`(?:$|\W)`, post.Message); matched {
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
	} else {
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
			SendMsgToDebuggingChannel("_"+config.LongName+" has **stopped** running_", "")
			os.Exit(0)
		}
	}()
}
