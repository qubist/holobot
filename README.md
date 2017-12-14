# Mattermost HoloBot

This code is based on : https://github.com/mattermost/mattermost-bot-sample-golang

## Usage

1 - setup the values in a config.yaml file like this:

``` yaml
Domain: "chat.holochain.net"
LongName: "Mattermost Bot Sample"
UserEmail: "your@email.here"
UserPassword: "somepasswordhere"
UserName: "holobot"
UserFirst: "holo"
UserLast: "bot"
TeamName: "team-it-should-run-on"
LogChannel: "debugging-for-sample-bot"

```

2 - Clone the GitHub repository to run the sample.
```
git clone https://github.com/qubist/holobot.git
cd holobot
```
3 - Start the Bot.
```
make run
```
You can verify the Bot is running when
  - `Server detected and is running version 3.X.X` appears on the command line.
  - `Mattermost Bot Sample has started running` is posted in the `Debugging For Sample Bot` channel.

## Test the Bot

1 - Log in to the Mattermost server as `bill@example.com` and `password1.`

2 - Join the `Debugging For Sample Bot` channel.

3 - Post a message in the channel such as `are you running?` to see if the Bot responds. You should see a response similar to `Yes I'm running` if the Bot is running.

## Stop the Bot

1 - In the terminal window, press `CTRL+C` to stop the bot. You should see `Mattermost Bot Sample has stopped running` posted in the `Debugging For Sample Bot` channel.
