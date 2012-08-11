package main

import (
	"fmt"
	"labix.org/v2/mgo"
	"mud/database"
	"mud/game"
	"mud/utils"
	"net"
	"strconv"
)

func login(session *mgo.Session, conn net.Conn) (string, error) {

	for {
		line, err := utils.GetUserInput(conn, "Username: ")

		if err != nil {
			return "", err
		}

		found, err := database.FindUser(session, line)

		if err != nil {
			return "", err
		}

		if !found {
			utils.WriteLine(conn, "User not found")
		} else {
			return line, nil
		}
	}

	panic("Unexpected code path")
	return "", nil
}

func newUser(session *mgo.Session, conn net.Conn) (string, error) {

	for {
		line, err := utils.GetUserInput(conn, "Desired username: ")

		if err != nil {
			return "", err
		}

		err = database.NewUser(session, line)
		if err == nil {
			return line, nil
		}

		utils.WriteLine(conn, err.Error())
	}

	panic("Unexpected code path")
	return "", nil
}

func newCharacter(session *mgo.Session, conn net.Conn, user string) (string, error) {
	// TODO: character slot limit
	for {
		line, err := utils.GetUserInput(conn, "Desired character name: ")

		if err != nil {
			return "", err
		}

		err = database.NewCharacter(session, user, line)

		if err == nil {
			return line, err
		}

		utils.WriteLine(conn, err.Error())
	}

	panic("Unexpected code path")
	return "", nil
}

func quit(session *mgo.Session, conn net.Conn) error {
	utils.WriteLine(conn, "Goodbye!")
	conn.Close()
	return nil
}

func mainMenu() Menu {

	menu := NewMenu("MUD")

	menu.AddAction("l", "[L]ogin")
	menu.AddAction("n", "[N]ew user")
	menu.AddAction("a", "[A]bout")
	menu.AddAction("q", "[Q]uit")

	return menu
}

func characterMenu(session *mgo.Session, user string) Menu {

	chars, _ := database.GetUserCharacters(session, user)

	menu := NewMenu("Character Select")
	menu.AddAction("n", "[N]ew character")
	if len(chars) > 0 {
		menu.AddAction("d", "[D]elete character")
	}

	for i, char := range chars {
		indexStr := strconv.Itoa(i + 1)
		actionText := fmt.Sprintf("[%v]%v", indexStr, utils.FormatName(char))
		menu.AddActionData(indexStr, actionText, char)
	}

	return menu

}

func deleteMenu(session *mgo.Session, user string) Menu {
	chars, _ := database.GetUserCharacters(session, user)

	menu := NewMenu("Delete character")

	menu.AddAction("c", "[C]ancel")

	for i, char := range chars {
		indexStr := strconv.Itoa(i + 1)
		actionText := fmt.Sprintf("[%v]%v", indexStr, utils.FormatName(char))
		menu.AddActionData(indexStr, actionText, char)
	}

	return menu
}

func handleConnection(session *mgo.Session, conn net.Conn) {

	defer conn.Close()
	defer session.Close()

	user := ""
	character := ""

	for {
		if user == "" {
			menu := mainMenu()
			choice, _, err := menu.Exec(conn)

			if err != nil {
				return
			}

			switch choice {
			case "l":
				var err error
				user, err = login(session, conn)
				if err != nil {
					return
				}
			case "n":
				var err error
				user, err = newUser(session, conn)
				if err != nil {
					return
				}
			case "q":
				quit(session, conn)
				return
			}

			if err != nil {
				return
			}
		} else if character == "" {
			menu := characterMenu(session, user)
			choice, charName, err := menu.Exec(conn)

			if err != nil {
				return
			}

			_, err = strconv.Atoi(choice)

			if err == nil {
				character = charName
			} else {
				switch choice {
				case "n":
					character, err = newCharacter(session, conn, user)
				case "d":
					deleteMenu := deleteMenu(session, user)
					deleteChoice, deleteCharName, err := deleteMenu.Exec(conn)

					_, err = strconv.Atoi(deleteChoice)

					if err == nil {
						database.DeleteCharacter(session, user, deleteCharName)
					} else {
					}
				}
			}
		} else {
			game.Exec(session, conn, character)
			user = ""
			character = ""
		}
	}
}

func main() {

	fmt.Printf("Connecting to database... ")
	session, err := mgo.Dial("localhost")

	utils.HandleError(err)

	fmt.Printf("done.\n")

	listener, err := net.Listen("tcp", ":8945")
	utils.HandleError(err)

	fmt.Printf("Server listening on port 8945\n")

	for {
		conn, err := listener.Accept()
		utils.HandleError(err)
		go handleConnection(session.Copy(), conn)
	}
}

// vim: nocindent