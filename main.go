package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"github.com/andersfylling/disgord"
	"github.com/andersfylling/disgord/std"
	"github.com/sirupsen/logrus"
	"os"
	"strconv"
	"strings"
)

var log = &logrus.Logger{
	Out:       os.Stderr,
	Formatter: new(logrus.TextFormatter),
	Hooks:     make(logrus.LevelHooks),
	Level:     logrus.WarnLevel,
}

var SteckbriefChannel disgord.Snowflake
var SteckbriefRole disgord.Snowflake
var MessageFetchLimit uint
var MinLength int

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func main() {

	SteckbriefChannel = disgord.ParseSnowflakeString(os.Getenv("STECKBRIEF_CHANNEL"))
	SteckbriefRole = disgord.ParseSnowflakeString(os.Getenv("STECKBRIEF_ROLE"))
	number, _ := strconv.ParseUint(getEnv("MESSAGE_FETCH_LIMIT", "100"), 10, 64)
	MessageFetchLimit = uint(number)
	MinLength, _ = strconv.Atoi(getEnv("MESSAGE_FETCH_LIMIT", "200"))

	discord := disgord.New(disgord.Config{
		ProjectName: "Steckbriefe",
		BotToken: os.Getenv("DISCORD_TOKEN"),
		Logger: log,
		Presence: &disgord.UpdateStatusPayload{
			Game: &disgord.Activity{
				Name: "schreibe !ping",
			},
		},
	})
	defer discord.Gateway().StayConnectedUntilInterrupted()

	log, _ := std.NewLogFilter(discord)
	filter, _ := std.NewMsgFilter(context.Background(), discord)
	filter.SetPrefix("!")

	discord.Gateway().WithMiddleware(
		filter.NotByBot,    // ignore bot messages
		filter.HasPrefix,   // read original
		log.LogMsg,         // log command message
		filter.StripPrefix, // write copy
	).MessageCreate(handleMessageSend)
}

// handleMessageSend is a handler that replies messages
func handleMessageSend(session disgord.Session, data *disgord.MessageCreate) {
	msg := data.Message

	// Whenever the message written is "ping", the bot replies "pong"
	if msg.Content == "ping" {
		_, err := msg.Reply(context.Background(), session, "pong")
		if err != nil {
			log.Error("error sending ping response ", err)
			return
		}
	}

	if strings.HasPrefix(msg.Content, "steckbrief") {

		// Check if 'steckbrief' is longer than MinLength
		if len(msg.Content) < MinLength {
			_, _ = msg.Reply(context.Background(), session, "Bitte füge mehr Informationen zu deinem Steckbrief hinzu :grin:")
			return
		}

		// Check if 'steckbrief' channel exists
		channel, err := session.Channel(SteckbriefChannel).Get()
		if err != nil {
			log.Error("Could not find 'steckbrief' channel! Please ensure the bot has the correct permissions and the channel exist!")
			_, _ = msg.Reply(context.Background(), session, "Es konnte kein Steckbrief Kanal gefunden werden! Bitte kontaktiere einen Systemadministator.")
			return
		}

		// Check if the user has the 'steckbrief' role
		hasRole := false
		member, err := session.Guild(msg.GuildID).Member(msg.Author.ID).Get()
		if err == nil {
			for _, role := range member.Roles {
				if role == SteckbriefRole {
					hasRole = true
				}
			}
		} else {
			log.Error(msg.Author.Username + "#" + msg.Author.Discriminator.String(), " error while fetching member: ", err)
		}

		// String magic - remove the command name and split each line into its own
		message := strings.TrimSpace(strings.TrimPrefix(strings.Replace(msg.Content, "`", "", -1), "steckbrief"))
		lines := strings.Split(message, "\n")

		name := lines[0] // Name is in line 1 (after the trimmed command)
		fields := make([]*disgord.EmbedField, 0) // Every line is a new embed field, but as we don't know if some lines are nil we start with 0 and append later
		fieldCounter := 0

		if len(lines) <= 1 {
			log.Warn(msg.Author.Username + "#" + msg.Author.Discriminator.String(), " has provided no fields in his/her 'steckbrief'")
			_, _ = msg.Reply(context.Background(), session, "Keine Informationen wurden im Steckbrief gefunden.")
			return
		}

		for i := 1; i < len(lines); i++ {

			// Ignore empty lines
			if len(strings.TrimSpace(lines[i])) < 1 {
				continue
			}

			splits := strings.Split(lines[i], ": ")

			// Check if there are non empty (or whitespace) key and value pairs
			if len(splits) != 2 || (len(splits) == 2 && ( len(strings.TrimSpace(splits[0])) < 1 || len(strings.TrimSpace(splits[1])) < 1 ) ) {
				log.Warn(msg.Author.Username + "#" + msg.Author.Discriminator.String(), " has a missing key value in category ", i, " in his/her 'steckbrief'")
				_, _ = msg.Reply(context.Background(), session, "Fehler im Steckbrief! Kategorie `" +  splits[0] + "` des Steckbriefs hat keinen zugehörigen Wert.")
				return
			}

			field := disgord.EmbedField{
				Name: splits[0],
				Value: splits[1],
			}
			fields = append(fields, &field)
			fieldCounter++
		}

		// Fetch and set the embed avatar to the users avatar
		avatarUrl, err := data.Message.Author.AvatarURL(64, false)
		if err != nil {
			log.Error("error fetching user avatar url ", err)
			return
		}

		thumbnail := disgord.EmbedThumbnail{
			URL: avatarUrl,
			Height: 64,
			Width: 64,
		}

		// Calculate embed color based on the users id
		hash := sha256.Sum256([]byte(msg.Author.ID.HexString()))
		hashNr := binary.BigEndian.Uint64(hash[:])
		number := strconv.FormatUint(hashNr, 10)[0:5]
		color, err := strconv.Atoi(number)
		if err != nil {
			color = 0
		}

		// Create embed with the fields provided
		embed := disgord.Embed{
			Title: "Steckbrief von " + name,
			Description: msg.Author.Mention(),
			Color:     color,
			Thumbnail: &thumbnail,
			Fields:    fields,
		}

		updateRequired := false
		var updateableMessage *disgord.Snowflake = nil

		// Check if a player already has sent a 'steckbrief'
		if hasRole {

			var messageParams = &disgord.GetMessagesParams{
				Limit: MessageFetchLimit,
			}

			messages, err := session.Channel(SteckbriefChannel).GetMessages(messageParams)
			if err != nil {
				log.Error("Error fetching message history: ", err)
				return
			}

			for _, message := range messages {
				if len(message.Embeds) <= 0 {
					log.Warn("Message has no embed!")
					continue
				}

				preSnowflake := strings.TrimPrefix(strings.TrimSuffix(message.Embeds[0].Description, ">"), "<@")
				if preSnowflake == ""{
					log.Warn("Empty embed description. Aborting!")
					continue
				}

				snowflake := disgord.ParseSnowflakeString(preSnowflake)
				steckbriefSender, err := session.User(snowflake).Get()
				if err != nil {
					log.Error("Error fetching user: ", err)
					continue
				}

				if steckbriefSender.ID == msg.Author.ID {
					updateRequired = true
					updateableMessage = &message.ID
					break
				}
			}
		}

		// Update 'steckbrief' if it already exists
		if updateRequired {
			if &updateableMessage != nil {
				_, err := session.Channel(SteckbriefChannel).Message(*updateableMessage).Update().SetEmbed(&embed).Execute()
				if err != nil {
					_, _ = msg.Reply(context.Background(), session, "Fehler beim Aktualisieren deines Steckbriefes.")
					log.Error("Error updating message: ", err)
				}
				_, _ = msg.Reply(context.Background(), session, "Dein Steckbrief wurde aktualisiert.")
				return
			}
		}

		// Send 'steckbrief' into the correct chat channel if it doesn't exist
		_, err = session.SendMsg(channel.ID, embed)
		if err != nil {
			log.Error("Error sending embed message ", err)
			return
		}

		err = session.Guild(msg.GuildID).Member(msg.Author.ID).AddRole(SteckbriefRole)
		if err != nil {
			log.Error(msg.Author.Username + "#" + msg.Author.Discriminator.String(), " error while giving 'steckbrief' role: ", err)
			_, _ = msg.Reply(context.Background(), session, "Fehler beim geben der Steckbriefrolle! Bitte wende dich an den Systemadministrator.")
			return
		}
	}
}