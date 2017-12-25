# Mattermost HoloBot

This code is based on : https://github.com/mattermost/mattermost-bot-sample-golang

## Getting Started

1 - Clone the GitHub repository.
```
git clone https://github.com/qubist/holobot.git
cd holobot
```

2 - Create a config file called `config.yaml` and setup the values like this:

``` yaml
Domain: "your.chatdomain.com"
LongName: "Mattermost Bot Sample"
UserEmail: "your@email.here"
UserPassword: "somepasswordhere"
UserName: "imabot"
UserFirst: "Im A"
UserLast: "Bot."
PublicTeamName: "name-of-public-team"
PrivateTeamName: "name-of-private-team"
DebuggingTeamName: "name-of-debugging-team"
LogChannel: "debugging-for-sample-bot"
Debugging: false

```
3 - Get the Mattermost server model package.
```
go get github.com/mattermost/mattermost-server/model
```

3 - Start the Bot.
```
make run
```
You can verify the Bot is running when
  - `Server detected and is running version 3.X.X` appears on the command line.
  - `Mattermost Bot Sample has started running` is posted in the `Debugging For Sample Bot` channel.

## Test the Bot

1 - Log in to the Mattermost server as `your@email.here` and `somepasswordhere`.

2 - Join the `Debugging For Sample Bot` channel.

## Stop the Bot

1 - In the terminal window, press `CTRL+C` to stop the bot. You should see `Mattermost Bot Sample has stopped running` posted in the `Debugging For Sample Bot` channel.
