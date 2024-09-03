import React from "react";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Chat } from "@/types/types";
import useAuthStore from "@/stores/user.store";
import { getInitials, bgColorsByInitials } from "@/lib/helpers";

interface ChatListItemProps {
  chat: Chat;
  onClick: () => void;
  chatId: string;
}

const ChatListItem: React.FC<ChatListItemProps> = ({
  chat,
  onClick,
  chatId,
}) => {
  const { user } = useAuthStore();
  const otherParticipant = chat.participants?.find((p) => p._id !== user?._id);
  const lastMessage =
    chat.messages && chat.messages.length > 0
      ? chat.messages[chat.messages.length - 1]
      : null;

  return (
    <div
      className={`flex items-center gap-4 rounded-md p-2 hover:bg-muted/50 cursor-pointer}
      ${chatId === chat._id ? "bg-muted" : ""}
        `}
      onClick={onClick}
    >
      <Avatar>
        <AvatarFallback
          className={`font-bold
            ${bgColorsByInitials(getInitials(otherParticipant?.name || "UU"))}
            `}
        >
          {getInitials(otherParticipant?.name || "UU")}
        </AvatarFallback>
      </Avatar>
      <div className="flex-grow overflow-hidden">
        <p className="text-sm font-medium truncate">
          {otherParticipant?.name || "Unactive User"}
        </p>
        <p className="text-xs text-muted-foreground truncate">
          {lastMessage ? lastMessage.content : "No messages yet"}
        </p>
      </div>
      {lastMessage && lastMessage.timestamp && (
        <p className="text-xs text-muted-foreground">
          {new Date(lastMessage.timestamp).toLocaleTimeString([], {
            hour: "2-digit",
            minute: "2-digit",
          })}
        </p>
      )}
    </div>
  );
};

export default ChatListItem;
