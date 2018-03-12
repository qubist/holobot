# Mattermost holobot _(holobot)_

[![standard-readme compliant](https://img.shields.io/badge/readme%20style-standard-brightgreen.svg?style=flat)](https://github.com/RichardLitt/standard-readme)
[![GitHub license](https://img.shields.io/github/license/qubist/holobot.svg)](https://github.com/qubist/holobot/blob/master/LICENSE.txt)
[![GitHub stars](https://img.shields.io/github/stars/qubist/holobot.svg)](https://github.com/qubist/holobot/stargazers)

>  A bot for Mattermost servers that can listen for events and perform actions.

## Table of Contents

- [Background](#background)
- [Install](#install)
	- [Dependencies](#dependencies)
- [Usage](#usage)
	- [Starting the Bot](#starting-the-bot)
	- [Testing the Bot](#testing-the-bot)
	- [Stopping the Bot](#stopping-the-bot)
- [Maintainer](#maintainer)
- [Contribute](#contribute)
- [License](#license)

## Background

This code is based on [mattermost-bot-sample-golang](https://github.com/mattermost/mattermost-bot-sample-golang) made by [mattermost](https://github.com/mattermost).

## Install

1. Clone the GitHub repository.
```
$ git clone https://github.com/qubist/holobot.git
$ cd holobot
```

2. Create a config file called `config.yaml` and setup the values like this:
```yaml
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

3. Get the Mattermost server model package.
```
$ go get github.com/mattermost/mattermost-server/model
```

### Dependencies
* [golang](https://golang.org/)

## Usage

### Starting the Bot
```
$ make run
```
You can verify the Bot is running when
  - `Server detected and is running version 3.X.X` appears on the command line.
  - `Mattermost Bot Sample has started running` is posted in the `Debugging For Sample Bot` channel.

### Testing the Bot
1. Log in to the Mattermost server using the login you defined in `config.yaml`. (`your@email.here` and `somepasswordhere` in the given example.)

2. Join the `Debugging For Sample Bot` channel.

### Stopping the Bot
1. In the terminal window, press `CTRL+C` to stop the bot. You should see `Mattermost Bot Sample has stopped running` posted in the `Debugging For Sample Bot` channel.

## Maintainer
[Will](https://github.com/qubist/) for now

[zippy](https://github.com/zippy/) has also maintained this software in the past

## Contribute
Questions can be asked in the [Holochain public Mattermost server](https://chat.holochain.net). I'd love your suggestions!

Pull requests are accepted. Contributions to the code would be greatly appreciated!

## License

[Apache License 2.0](/LICENSE.txt)
