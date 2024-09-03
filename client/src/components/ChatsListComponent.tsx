import React from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import ChatListItem from "./ChatListItem";
import { Plus } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useNavigate } from "react-router-dom";
import { Skeleton } from "@/components/ui/skeleton";

interface ChatsListComponentProps {
  onNewChat: () => void;
  chats: any[];
  isLoading: boolean;
  error: string | null;
  chatId: string;
}

const ChatsListComponent: React.FC<ChatsListComponentProps> = ({
  onNewChat,
  chats,
  isLoading,
  error,
  chatId,
}) => {
  const navigate = useNavigate();

  return (
    <div className="bg-muted/20 p-3 border-r flex flex-col h-screen">
      <div className="flex items-center justify-between space-x-4 mb-4">
        <div className="font-medium text-sm">Chats</div>
        <Button
          variant="ghost"
          size="icon"
          className="rounded-full w-8 h-8"
          onClick={onNewChat}
        >
          <Plus className="h-4 w-4" />
          <span className="sr-only">New chat</span>
        </Button>
      </div>
      <Input placeholder="Search" className="mb-4" />
      <ScrollArea className="flex-grow">
        <div className="space-y-2">
          {isLoading ? (
            <div className="space-y-2 text-center">
              <p className="text-muted-foreground text-sm">Loading chats</p>
              <Skeleton className="w-full h-16" />
              <Skeleton className="w-full h-16" />
              <Skeleton className="w-full h-16" />
              <Skeleton className="w-full h-16" />
              <Skeleton className="w-full h-16" />
              <Skeleton className="w-full h-16" />
            </div>
          ) : error ? (
            <div className="text-center text-sm text-red-500">{error}</div>
          ) : chats.length > 0 ? (
            chats.map((chat) => (
              <ChatListItem
                key={chat._id}
                chatId={chatId}
                chat={chat}
                onClick={() => {
                  navigate(`/chats/${chat._id}`);
                }}
              />
            ))
          ) : (
            <>
              <div className="text-center text-muted-foreground w-2/3 m-auto">
                <span className="text-sm m-auto">
                  No chats yet, click the plus button to start a new chat.
                </span>
              </div>
            </>
          )}
        </div>
      </ScrollArea>
    </div>
  );
};

export default ChatsListComponent;
