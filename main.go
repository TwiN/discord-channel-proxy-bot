package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/TwinProduction/discord-channel-proxy-bot/database"
	"github.com/TwinProduction/gocache"
	"github.com/bwmarrin/discordgo"
)

var (
	token               = os.Getenv("DISCORD_BOT_TOKEN")
	botCommandPrefix    = "!"
	pendingBindRequests = gocache.NewCache().WithMaxSize(1000)

	killChannel chan os.Signal
)

func main() {
	if err := database.Initialize("data.db"); err != nil {
		panic(err)
	}
	bot, err := Connect(token)
	if err != nil {
		panic(err)
	}
	bot.AddHandler(HandleMessage)
	_ = pendingBindRequests.StartJanitor()
	defer pendingBindRequests.StopJanitor()
	waitUntilTermination()
}

func waitUntilTermination() {
	killChannel = make(chan os.Signal, 1)
	signal.Notify(killChannel, syscall.SIGTERM)
	<-killChannel
}

// Connect starts a Discord session
func Connect(discordToken string) (*discordgo.Session, error) {
	discordgo.MakeIntent(discordgo.IntentsGuildMessageReactions)
	session, err := discordgo.New("Bot " + discordToken)
	if err != nil {
		return nil, err
	}
	err = session.Open()
	return session, err
}

func HandleMessage(bot *discordgo.Session, message *discordgo.MessageCreate) {
	if message.Author.Bot || message.Author.ID == bot.State.User.ID {
		return
	}
	if strings.HasPrefix(message.Content, botCommandPrefix) {
		command := strings.Replace(strings.Split(message.Content, " ")[0], botCommandPrefix, "", 1)
		query := strings.TrimSpace(strings.Replace(message.Content, botCommandPrefix+command, "", 1))
		command = strings.ToLower(command)
		log.Printf("[HandleMessage] command=%s; arguments=%s", command, query)
		switch command {
		case "bind":
			HandleBind(bot, message.ChannelID, query)
		case "unbind":
			HandleUnbind(bot, message.ChannelID)
		case "clear", "clean", "wipe", "nuke":
			HandleClear(bot, message.Message)
		}
	} else {
		if otherChannelID, err := database.GetOtherChannelIDFromConnection(message.ChannelID); err == nil {
			var attachments string
			for _, attachment := range message.Attachments {
				attachments += attachment.URL
			}
			if len(message.Content) > 0 {
				attachments = " " + attachments
			}
			_, err = bot.ChannelMessageSend(otherChannelID, message.Content+attachments)
			if err == nil {
				_ = bot.MessageReactionAdd(message.ChannelID, message.ID, "✅")
			} else {
				_ = bot.MessageReactionAdd(message.ChannelID, message.ID, "❌")
			}
		} else {
			if err != database.ErrNotFound {
				log.Println("[HandleMessage] Failed to get otherChannelID:", err.Error())
			}
		}
	}
}

func HandleClear(bot *discordgo.Session, message *discordgo.Message) {
	messages, err := bot.ChannelMessages(message.ChannelID, 100, message.ID, "", "")
	if err != nil {
		log.Println("[HandleClear] Failed to retrieve messages in channel:", err.Error())
		return
	}
	ids := make([]string, 0, len(messages)+1)
	ids[0] = message.ID
	for _, m := range messages {
		ids = append(ids, m.ID)
	}
	if err := bot.ChannelMessagesBulkDelete(message.ChannelID, ids); err != nil {
		log.Println("[HandleClear] Failed to delete messages:", err.Error())
		return
	}
	if len(ids) == 100 {
		HandleClear(bot, message)
	}
}

func HandleBind(bot *discordgo.Session, fromChannelID, toChannelID string) {
	if fromChannelID == toChannelID {
		_ = sendEmbed(bot, fromChannelID, "You can't bind a channel to itself", "")
		return
	}
	// Check if the target has already sent a binding request
	_, exists := pendingBindRequests.Get(toChannelID + "-" + fromChannelID)
	if exists {
		pendingBindRequests.Delete(toChannelID + "-" + fromChannelID)
		// Since a binding request originating from toChannelID has already been sent targeting fromChannelID,
		// both parties have agreed therefore the connection has been established
		_ = sendEmbed(bot, fromChannelID, "Connection successfully established with "+toChannelID, "")
		_ = sendEmbed(bot, toChannelID, "Connection successfully established with "+fromChannelID, "")
		if err := database.CreateConnection(fromChannelID, toChannelID); err == nil {
			log.Println("[HandleBind] Created connection between", fromChannelID, "and", toChannelID)
		} else {
			log.Printf("[HandleBind] Failed to create connection between %s and %s: %s", fromChannelID, toChannelID, err.Error())
		}
		return
	}
	err := sendEmbed(bot, toChannelID, "Binding request from "+fromChannelID, fmt.Sprintf("You have 60 seconds to reply `%sbind %s`", botCommandPrefix, fromChannelID))
	if err != nil {
		_ = sendEmbed(bot, fromChannelID, "Failed to send binding request", "```"+err.Error()+"```")
		return
	}
	pendingBindRequests.SetWithTTL(fromChannelID+"-"+toChannelID, "BINDING_REQUEST", time.Minute)
	_ = sendEmbed(bot, fromChannelID, "Binding request sent", "")
}

func HandleUnbind(bot *discordgo.Session, channelID string) {
	err := database.DeleteConnectionByChannelID(channelID)
	if err != nil {
		_ = sendEmbed(bot, channelID, "Failed to unbind channel", "```"+err.Error()+"```")
		return
	}
	_ = sendEmbed(bot, channelID, "Channel unbound successfully", "")
}

func sendEmbed(bot *discordgo.Session, channelID, title, description string) error {
	_, err := bot.ChannelMessageSendEmbed(channelID, &discordgo.MessageEmbed{
		Title:       title,
		Description: description,
	})
	return err
}
