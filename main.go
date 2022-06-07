package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/TwiN/discord-channel-proxy-bot/database"
	"github.com/TwiN/gocache/v2"
	"github.com/bwmarrin/discordgo"
)

var (
	token               = os.Getenv("DISCORD_BOT_TOKEN")
	botCommandPrefix    = os.Getenv("COMMAND_PREFIX")
	pendingBindRequests = gocache.NewCache().WithMaxSize(1000)

	killChannel chan os.Signal
)

func init() {
	if len(botCommandPrefix) == 0 {
		botCommandPrefix = "!"
	}
}

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
		log.Printf("[HandleMessage] channel=%s; command=%s; arguments=%s", message.ChannelID, command, query)
		switch command {
		case "bind":
			HandleBind(bot, message.ChannelID, query)
		case "unbind":
			HandleUnbind(bot, message.ChannelID)
		case "clear", "clean", "wipe", "nuke":
			HandleClear(bot, message.Message, false)
		case "clearother":
			HandleClear(bot, message.Message, true)
		case "lock":
			HandleLock(bot, message.Message, false)
		case "unlock":
			HandleLock(bot, message.Message, true)
		case "pull":
			HandlePull(bot, message.Message)
		}
	} else {
		if otherChannelID, err := database.GetOtherChannelIDFromConnection(message.ChannelID); err == nil {
			if database.IsChannelLocked(otherChannelID) {
				_ = bot.MessageReactionAdd(message.ChannelID, message.ID, "⌛")
				log.Printf("[HandleMessage] Not proxying message from=%s to=%s because channel=%s is locked", message.ChannelID, otherChannelID, otherChannelID)
				return
			}
			if err := proxyMessage(bot, message.Message, otherChannelID); err != nil {
				_ = bot.MessageReactionAdd(message.ChannelID, message.ID, "❌")
			} else {
				_ = bot.MessageReactionAdd(message.ChannelID, message.ID, "✅")
			}
		} else {
			if err != database.ErrNotFound {
				log.Println("[HandleMessage] Failed to get otherChannelID:", err.Error())
			}
		}
	}
}

func HandlePull(bot *discordgo.Session, message *discordgo.Message) {
	destinationChannelID := message.ChannelID
	sourceChannelID, err := database.GetOtherChannelIDFromConnection(destinationChannelID)
	if err != nil {
		log.Println("[HandlePull] Unable to get other channel ID:", err.Error())
		return
	}
	messages, err := bot.ChannelMessages(sourceChannelID, 50, "", "", "")
	var messagesToSend []*discordgo.Message
	for _, m := range messages {
		if m.Author.ID == bot.State.User.ID || strings.HasPrefix(m.Content, botCommandPrefix) {
			// Ignore messages from bot & commands
			continue
		}
		pending := false
		for _, reaction := range m.Reactions {
			if reaction.Emoji.Name == "⌛" {
				pending = true
				break
			}
		}
		if !pending {
			continue
		}
		messagesToSend = append([]*discordgo.Message{m}, messagesToSend...)
	}
	for _, messageToSend := range messagesToSend {
		if err := proxyMessage(bot, messageToSend, destinationChannelID); err != nil {
			log.Println("[HandlePull] Unable to send message:", err.Error())
			_ = bot.MessageReactionAdd(messageToSend.ChannelID, messageToSend.ID, "❌")
		} else {
			_ = bot.MessageReactionRemove(messageToSend.ChannelID, messageToSend.ID, "⌛", bot.State.User.ID)
			_ = bot.MessageReactionAdd(messageToSend.ChannelID, messageToSend.ID, "✅")
		}
	}
	_ = bot.ChannelMessageDelete(message.ChannelID, message.ID)
}

func proxyMessage(bot *discordgo.Session, message *discordgo.Message, targetChannelID string) error {
	var attachments string
	for _, attachment := range message.Attachments {
		attachments += " " + attachment.URL
	}
	if len(message.Content) > 0 {
		attachments = " " + attachments
	}
	log.Printf("[proxyMessage] Proxying message from=%s to=%s", message.ChannelID, targetChannelID)
	_, err := bot.ChannelMessageSend(targetChannelID, message.Content+attachments)
	return err
}

func HandleLock(bot *discordgo.Session, message *discordgo.Message, unlock bool) {
	var action string
	if unlock {
		action = "unlock"
	} else {
		action = "lock"
	}
	err := database.LockChannel(message.ChannelID, unlock)
	if err != nil {
		_ = sendEmbed(bot, message.ChannelID, "Failed to "+action+" channel", err.Error())
		return
	}
	_ = sendEmbed(bot, message.ChannelID, "Channel has been "+action+"ed", "")
}

func HandleClear(bot *discordgo.Session, message *discordgo.Message, target bool) {
	var err error
	var messages []*discordgo.Message
	if target {
		otherChannelId, err := database.GetOtherChannelIDFromConnection(message.ChannelID)
		if err != nil {
			log.Println("[HandleClear] Unable to get other channel ID:", err.Error())
			return
		}
		messages, err = bot.ChannelMessages(otherChannelId, 99, "", "", "")
	} else {
		messages, err = bot.ChannelMessages(message.ChannelID, 99, message.ID, "", "")
	}
	if err != nil {
		log.Println("[HandleClear] Failed to retrieve messages in channel:", err.Error())
		return
	}
	if len(messages) == 0 {
		return
	}
	ids := make([]string, 0, len(messages)+1)
	ids = append(ids, message.ID)
	for _, m := range messages {
		ids = append(ids, m.ID)
	}
	if err := bot.ChannelMessagesBulkDelete(messages[0].ChannelID, ids); err != nil {
		log.Println("[HandleClear] Failed to delete messages:", err.Error())
		if strings.Contains(err.Error(), "can only bulk delete messages that are under 14 days old") {
			// Try to delete them one at a time.
			log.Println("[HandleClear] Deleting messages one at a time instead")
			for _, id := range ids {
				if err := bot.ChannelMessageDelete(messages[0].ChannelID, id); err != nil {
					log.Println("[HandleClear] Failed to delete message:", err.Error())
					return
				}
			}
		}
		return
	}
	if len(ids) == 100 {
		// If there's 100 results, there's probably more messages left to delete
		HandleClear(bot, message, target)
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
