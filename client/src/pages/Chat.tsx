import React, { useState, useEffect } from "react";
import ChatsListComponent from "@/components/ChatsListComponent";
import NewChatComponent from "@/components/NewChatComponent";
import { Button } from "@/components/ui/button";
import { useParams, useNavigate } from "react-router-dom";
import ChatConversationComponent from "@/components/ChatConversationComponent";
import { toast } from "sonner";
import api from "@/api/axios.config";

const ChatPage: React.FC = () => {
  const [isNewChatOpen, setIsNewChatOpen] = useState(false);
  const { chatId } = useParams<{ chatId: string }>();
  const navigate = useNavigate();
  const [chats, setChats] = useState([]);
  const [error, setError] = useState("");
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    fetchChats();
  }, []);

  useEffect(() => {
    if (error) {
      navigate("/chats");
      toast.error("Chat not found");
    }
  }, [error]); // Add error to the dependency array

  const fetchChats = async () => {
    setIsLoading(true);
    try {
      const response = await api.get("chats/");
      setChats(response.data);
    } catch (error) {
      setError("Something went wrong while fetching chats" + error);
    } finally {
      setIsLoading(false);
    }
  };

  const handleNewChat = () => {
    setIsNewChatOpen(true);
  };

  const handleCloseNewChat = () => {
    setIsNewChatOpen(false);
  };

  return (
    <div className="grid grid-cols-[300px_1fr] h-full">
      <ChatsListComponent
        onNewChat={handleNewChat}
        chats={chats}
        isLoading={isLoading}
        error={error}
        chatId={chatId || ""}
      />
      {chatId ? (
        <ChatConversationComponent fetchChats={fetchChats} chatId={chatId} />
      ) : (
        <div className="flex items-center justify-center h-full text-muted-foreground flex-col bg-secondary bg-opacity-20">
          Select a Chat or Start a New Conversation
          <Button onClick={handleNewChat} className="mt-4">
            New Chat
          </Button>
        </div>
      )}

      <NewChatComponent
        isOpen={isNewChatOpen}
        onClose={handleCloseNewChat}
        fetchChats={fetchChats}
      />
    </div>
  );
};

export default ChatPage;
