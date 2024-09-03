import React, {
  useState,
  useEffect,
  useRef,
  FormEvent,
  KeyboardEvent,
} from "react";
import { useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Send, MessageSquare, Trash2 } from "lucide-react";
import MessageBubble from "./MessageBubble";
import { ScrollArea } from "@/components/ui/scroll-area";
import useAuthStore from "@/stores/user.store";
import { getInitials, bgColorsByInitials } from "@/lib/helpers";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import api from "@/api/axios.config";
import { toast } from "sonner";
import { Message } from "@/types/types";

// Define types for the chat and message
interface Participant {
  _id: string;
  name: string;
}

interface Chat {
  _id: string;
  participants: Participant[];
  messages: Message[];
}

const ChatConversationComponent: React.FC<{
  chatId: string | undefined;
  fetchChats: () => void;
}> = ({ chatId, fetchChats }) => {
  const navigate = useNavigate();
  const { user } = useAuthStore();
  const [newMessage, setNewMessage] = useState("");
  const scrollAreaRef = useRef<HTMLDivElement>(null);
  const [currentChat, setCurrentChat] = useState<Chat | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  // Fetch chat data
  useEffect(() => {
    if (!chatId) return;
    const fetchChat = async (chatId: string) => {
      try {
        setIsLoading(true);
        const response = await api.get(`chats/${chatId}`);
        setCurrentChat(response.data);
      } catch (error) {
        toast.error("Failed to fetch chat");
        navigate("/chats");
      } finally {
        setIsLoading(false);
      }
    };
    fetchChat(chatId);
  }, [chatId, navigate]);

  const sendMessage = async (chatId: string, content: string) => {
    try {
      const response = await api.post(`chats/${chatId}/messages`, { content });
      const newMessage = response.data as Message;

      console.log(user?._id, newMessage.sender);
      setCurrentChat((prevChat) =>
        prevChat
          ? {
              ...prevChat,
              messages: [...prevChat.messages, newMessage],
            }
          : null
      );

      scrollAreaRef.current?.scrollTo({
        top: scrollAreaRef.current.scrollHeight,
        behavior: "smooth",
      });
    } catch (error) {
      toast.error("Failed to send message");
    }
  };

  const handleSendMessage = async (e: FormEvent) => {
    e.preventDefault();
    if (!newMessage.trim() || !chatId) return;

    await sendMessage(chatId, newMessage.trim());
    setNewMessage("");
  };

  const deleteMessage = async (chatId: string, messageId: string) => {
    try {
      await api.delete(`chats/${chatId}/messages/${messageId}`);
      setCurrentChat((prevChat) =>
        prevChat
          ? {
              ...prevChat,
              messages: prevChat.messages.filter(
                (msg) => msg._id !== messageId
              ),
            }
          : null
      );
    } catch (error) {
      toast.error("Failed to delete message");
    }
  };

  const deleteChat = async (chatId: string) => {
    try {
      await api.delete(`chats/${chatId}`);
      navigate("/chats");
      fetchChats();
    } catch (error) {
      toast.error("Failed to delete chat");
    }
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if ((e.metaKey || e.ctrlKey || e.shiftKey) && e.key === "Enter") {
      e.preventDefault();
      handleSendMessage(e as FormEvent);
    }
  };

  const otherParticipant = currentChat?.participants.find(
    (participant) => participant._id !== user?._id
  );

  if (!currentChat) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="text-center">
          <MessageSquare className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
          <h2 className="text-lg font-semibold mb-2">No Chat Selected</h2>
          <p className="text-muted-foreground">
            Select a chat or start a new one
          </p>
        </div>
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="text-center">
          <MessageSquare className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
          <h2 className="text-lg font-semibold mb-2">Loading Chat...</h2>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-screen">
      <div className="p-3 flex items-center justify-between border-b">
        <div className="flex items-center">
          <Avatar className="mr-3">
            <AvatarFallback
              className={`font-bold ${bgColorsByInitials(
                getInitials(otherParticipant?.name || "UU")
              )}`}
            >
              {getInitials(otherParticipant?.name || "UU")}
            </AvatarFallback>
          </Avatar>
          <div>
            <p className="font-bold">
              {otherParticipant?.name || "Unknown User"}
            </p>
          </div>
        </div>
        <AlertDialog>
          <AlertDialogTrigger asChild>
            <Button variant="ghost" size="icon">
              <Trash2 className="h-4 w-4" />
            </Button>
          </AlertDialogTrigger>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Delete Chat</AlertDialogTitle>
              <AlertDialogDescription>
                Are you sure you want to delete this chat? This action cannot be
                undone.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction onClick={() => deleteChat(chatId!)}>
                Delete
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
      <ScrollArea className="flex-grow p-4" ref={scrollAreaRef}>
        <div className="space-y-4">
          {currentChat.messages.length > 0 ? (
            currentChat.messages.map((message) => (
              <MessageBubble
                key={message._id}
                message={message}
                isOwnMessage={message.sender._id === user?._id}
                onDelete={() => deleteMessage(chatId!, message._id)}
              />
            ))
          ) : (
            <div className="text-center text-sm text-muted-foreground">
              Nothing to show, start the conversation by sending a message to{" "}
              {otherParticipant?.name || "the participant"}...
            </div>
          )}
        </div>
      </ScrollArea>
      <form onSubmit={handleSendMessage} className="p-3 border-t">
        <div className="flex items-center space-x-2">
          <Textarea
            value={newMessage}
            onChange={(e) => setNewMessage(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Type a message... (Cmd/Ctrl + Enter or Shift + Enter to send)"
            className="h-24 max-h-64"
          />
          <Button type="submit" size="icon">
            <Send className="h-4 w-4" />
          </Button>
        </div>
      </form>
    </div>
  );
};

export default ChatConversationComponent;
