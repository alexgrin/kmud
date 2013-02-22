package session

import (
	"fmt"
	"io"
	"kmud/database"
	"kmud/model"
	"kmud/utils"
	"labix.org/v2/mgo/bson"
	"strings"
	"time"
)

type Session struct {
	conn   io.ReadWriter
	user   *database.User
	player *database.Character
	room   *database.Room
	zone   *database.Zone

	userInputChannel chan string
	promptChannel    chan string

	inputModeChannel chan userInputMode
	panicChannel     chan interface{}

	eventChannel chan model.Event
}

func NewSession(conn io.ReadWriter, user *database.User, player *database.Character) Session {
	var session Session
	session.conn = conn
	session.user = user
	session.player = player
	session.room = model.M.GetRoom(player.GetRoomId())
	session.zone = model.M.GetZone(session.room.GetZoneId())

	session.userInputChannel = make(chan string)
	session.promptChannel = make(chan string)

	session.inputModeChannel = make(chan userInputMode)
	session.panicChannel = make(chan interface{})

	session.eventChannel = model.Register(session.player)

	return session
}

type userInputMode int

const (
	CleanUserInput userInputMode = iota
	RawUserInput   userInputMode = iota
)

func toggleExitMenu(cm utils.ColorMode, room *database.Room) utils.Menu {
	onOrOff := func(direction database.ExitDirection) string {
		text := "Off"
		if room.HasExit(direction) {
			text = "On"
		}
		return utils.Colorize(cm, utils.ColorBlue, text)
	}

	menu := utils.NewMenu("Edit Exits")

	menu.AddAction("n", "[N]orth: "+onOrOff(database.DirectionNorth))
	menu.AddAction("ne", "[NE]North East: "+onOrOff(database.DirectionNorthEast))
	menu.AddAction("e", "[E]ast: "+onOrOff(database.DirectionEast))
	menu.AddAction("se", "[SE]South East: "+onOrOff(database.DirectionSouthEast))
	menu.AddAction("s", "[S]outh: "+onOrOff(database.DirectionSouth))
	menu.AddAction("sw", "[SW]South West: "+onOrOff(database.DirectionSouthWest))
	menu.AddAction("w", "[W]est: "+onOrOff(database.DirectionWest))
	menu.AddAction("nw", "[NW]North West: "+onOrOff(database.DirectionNorthWest))
	menu.AddAction("u", "[U]p: "+onOrOff(database.DirectionUp))
	menu.AddAction("d", "[D]own: "+onOrOff(database.DirectionDown))

	return menu
}

func npcMenu(room *database.Room) utils.Menu {
	npcs := model.M.NpcsIn(room)

	menu := utils.NewMenu("NPCs")

	menu.AddAction("n", "[N]ew")

	for i, npc := range npcs {
		index := i + 1
		actionText := fmt.Sprintf("[%v]%v", index, npc.PrettyName())
		menu.AddActionData(index, actionText, npc.GetId())
	}

	return menu
}

func specificNpcMenu(npcId bson.ObjectId) utils.Menu {
	npc := model.M.GetCharacter(npcId)
	menu := utils.NewMenu(npc.PrettyName())
	menu.AddAction("r", "[R]ename")
	menu.AddAction("d", "[D]elete")
	menu.AddAction("c", "[C]onversation")
	return menu
}

func (session *Session) Exec() {
	defer model.Unregister(session.eventChannel)

	session.printLineColor(utils.ColorWhite, "Welcome, "+session.player.PrettyName())
	session.printRoom()

	// Main routine in charge of actually reading input from the connection object,
	// also has built in throttling to limit how fast we are allowed to process
	// commands from the user. 
	go func() {
		defer func() {
			if r := recover(); r != nil {
				session.panicChannel <- r
			}
		}()

		lastTime := time.Now()

		delay := time.Duration(200) * time.Millisecond

		for {
			mode := <-session.inputModeChannel
			prompt := utils.Colorize(session.user.GetColorMode(), utils.ColorWhite, <-session.promptChannel)
			input := ""

			switch mode {
			case CleanUserInput:
				input = utils.GetUserInput(session.conn, prompt)
			case RawUserInput:
				input = utils.GetRawUserInput(session.conn, prompt)
			default:
				panic("Unhandled case in switch statement (userInputMode)")
			}

			diff := time.Since(lastTime)

			if diff < delay {
				time.Sleep(delay - diff)
			}

			session.userInputChannel <- input
			lastTime = time.Now()
		}
	}()

	// Main loop
	for {
		input := session.getUserInput(RawUserInput, prompt())
		if input == "" || input == "logout" {
			return
		}
		if strings.HasPrefix(input, "/") {
			session.handleCommand(utils.Argify(input[1:]))
		} else {
			session.handleAction(utils.Argify(input))
		}
	}
}

func (session *Session) printString(data string) {
	io.WriteString(session.conn, data)
}

func (session *Session) printLineColor(color utils.Color, line string, a ...interface{}) {
	utils.WriteLine(session.conn, utils.Colorize(session.user.GetColorMode(), color, fmt.Sprintf(line, a...)))
}

func (session *Session) printLine(line string, a ...interface{}) {
	session.printLineColor(utils.ColorWhite, line, a...)
}

func (session *Session) printError(err string, a ...interface{}) {
	session.printLineColor(utils.ColorRed, err, a...)
}

func (session *Session) printRoom() {
	playerList := model.M.PlayersIn(session.room, session.player)
	npcList := model.M.NpcsIn(session.room)
	session.printLine(session.room.ToString(database.ReadMode, session.user.GetColorMode(),
		playerList, npcList, model.M.GetItems(session.room.GetItemIds())))
}

func (session *Session) printRoomEditor() {
	session.printLine(session.room.ToString(database.EditMode, session.user.GetColorMode(), nil, nil, nil))
}

func (session *Session) clearLine() {
	utils.ClearLine(session.conn)
}

func prompt() string {
	return "> "
}

// Same behavior as menu.Exec(), except that it uses getUserInput
// which doesn't block the event loop while waiting for input
func (session *Session) execMenu(menu utils.Menu) (string, bson.ObjectId) {
	choice := ""
	var data bson.ObjectId

	for {
		menu.Print(session.conn, session.user.GetColorMode())
		choice = session.getUserInput(CleanUserInput, menu.GetPrompt())
		if menu.HasAction(choice) || choice == "" {
			data = menu.GetData(choice)
			break
		}
	}
	return choice, data
}

// getUserInput allows us to retrieve user input in a way that doesn't block the
// event loop by using channels and a separate Go routine to grab
// either the next user input or the next event.
func (session *Session) getUserInput(inputMode userInputMode, prompt string) string {
	session.inputModeChannel <- inputMode
	session.promptChannel <- prompt

	for {
		select {
		case input := <-session.userInputChannel:
			return input
		case event := <-session.eventChannel:
			message := session.processEvent(event)
			if message != "" {
				session.clearLine()
				session.printLine(message)
				session.printString(prompt)
			}
		case quitMessage := <-session.panicChannel:
			panic(quitMessage)
		}
	}
	panic("Unexpected code path")
}

func (session *Session) processEvent(event model.Event) string {
	message := event.ToString(session.player)

	switch event.Type() {
	case model.RoomUpdateEventType:
		roomEvent := event.(model.RoomUpdateEvent)
		if roomEvent.Room.GetId() == session.room.GetId() {
			session.room = roomEvent.Room
		}
	}

	return message
}

// vim: nocindent