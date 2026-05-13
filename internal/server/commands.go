package server

import (
	"conner/internal/protocol"
	"strings"
)

type CommandHandler func(s *Server, client *Client, args []string)

type CommandRegistry struct {
	handlers map[string]CommandHandler
}

func NewCommandRegistry() *CommandRegistry {
	r := &CommandRegistry{
		handlers: make(map[string]CommandHandler),
	}
	r.registerDefaults()
	return r
}

func (r *CommandRegistry) Register(name string, h CommandHandler) {
	r.handlers[name] = h
}

func (r *CommandRegistry) Handle(s *Server, client *Client, input string) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return
	}

	cmdName := parts[0]
	if h, ok := r.handlers[cmdName]; ok {
		h(s, client, parts[1:])
	} else {
		s.SendSystemMessage(client, "Unknown command: "+cmdName)
	}
}

func (r *CommandRegistry) registerDefaults() {
	r.Register("/list", handleList)
	r.Register("/private", handlePrivate)
	r.Register("/ann", handleAnn)
	r.Register("/op", handleOp)
	r.Register("/approve", handleApprove)
	r.Register("/block", handleBlock)
	r.Register("/blacklist", handleBlock)
	r.Register("/kick", handleKick)
}

func handleList(s *Server, client *Client, args []string) {
	var users []string
	for _, c := range s.ClientManager.GetAllClients() {
		if c.State == client.State {
			users = append(users, c.Nickname)
		}
	}
	s.SendSystemMessage(client, "Online: "+strings.Join(users, ", "))
}

func handlePrivate(s *Server, client *Client, args []string) {
	if len(args) < 2 {
		s.SendSystemMessage(client, "Usage: /private <nick> <message>")
		return
	}
	targetNick := args[0]
	text := strings.Join(args[1:], " ")
	target := s.ClientManager.GetClientByNickname(targetNick)
	if target != nil && target.State == client.State {
		pm := protocol.CreateMessage("PRIVATE", "[PM from "+client.Nickname+"]: "+text, client.Nickname)
		s.SendMessage(target, pm)
		s.SendSystemMessage(client, "PM sent to "+targetNick)
	} else {
		s.SendSystemMessage(client, "User not found: "+targetNick)
	}
}

func handleAnn(s *Server, client *Client, args []string) {
	if !client.IsAdmin {
		s.SendSystemMessage(client, "Admin only.")
		return
	}
	if len(args) == 0 {
		return
	}
	text := strings.Join(args, " ")
	s.BroadcastAnnouncement(text)
}

func handleOp(s *Server, client *Client, args []string) {
	if !client.IsAdmin || len(args) == 0 {
		s.SendSystemMessage(client, "Unauthorized or missing argument.")
		return
	}
	target := s.ClientManager.GetClientByNickname(args[0])
	if target != nil {
		target.IsAdmin = true
		s.SendSystemMessage(target, "You have been granted admin privileges.")
		s.SendSystemMessage(client, args[0]+" is now an admin.")
	}
}

func handleApprove(s *Server, client *Client, args []string) {
	if !client.IsAdmin || len(args) == 0 {
		return
	}
	if s.ApproveClient(args[0]) {
		s.SendSystemMessage(client, args[0]+" approved.")
	} else {
		s.SendSystemMessage(client, "User not found: "+args[0])
	}
}

func handleBlock(s *Server, client *Client, args []string) {
	if !client.IsAdmin || len(args) == 0 {
		return
	}
	if s.BlockClient(args[0]) {
		s.SendSystemMessage(client, args[0]+" blocked.")
	} else {
		s.SendSystemMessage(client, "User not found: "+args[0])
	}
}

func handleKick(s *Server, client *Client, args []string) {
	if !client.IsAdmin || len(args) == 0 {
		return
	}
	target := s.ClientManager.GetClientByNickname(args[0])
	if target != nil {
		s.removeClient(target)
		s.SendSystemMessage(client, args[0]+" kicked.")
	}
}
