# discord-channel-proxy-bot
This application allows cross-server communication by binding two text channels in two different servers.

This is an experimental project.


## Usage
To invite the bot in the server: `https://discordapp.com/oauth2/authorize?client_id=<YOUR_BOT_CLIENT_ID>&scope=bot&permissions=108608`

To bind a channel from a different server:
```
!bind CHANNEL_ID
```
where `CHANNEL_ID` is the external text channel id. The request will be sent to the target channel. 

Note that the bot must be present in both servers.

To unbind a channel, you can simply type `!unbind`.

To wipe all messages in a channel, type `!clear`.


## TODO
- !lock
- !unlock
- !status (returns whether the channel is bound and whether it's locked)
- !autoclean
