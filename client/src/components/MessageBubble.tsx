import React from "react";
import { Message } from "@/types/types";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { getInitials, bgColorsByInitials } from "@/lib/helpers";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";

interface MessageBubbleProps {
  message: Message;
  isOwnMessage: boolean;
  onDelete: () => void;
}

const MessageBubble: React.FC<MessageBubbleProps> = ({
  message,
  isOwnMessage,
  onDelete,
}) => {
  const senderName = message?.sender?.name || "ERROR";
  return (
    <div className={`flex ${isOwnMessage ? "justify-end" : "justify-start"}`}>
      <div
        className={`flex ${
          isOwnMessage ? "flex-row-reverse" : "flex-row"
        } items-end`}
      >
        <Avatar className="w-8 h-8">
          <AvatarFallback
            className={`font-bold ${bgColorsByInitials(
              getInitials(senderName)
            )}`}
          >
            {getInitials(senderName)}
          </AvatarFallback>
        </Avatar>
        <div
          className={`mx-2 p-3 rounded-lg ${
            isOwnMessage ? "bg-primary text-primary-foreground" : "bg-muted"
          }`}
        >
          <p>{message.content || "No content"}</p>
          <p className="text-xs mt-1 opacity-70">
            {new Date(message.timestamp).toLocaleTimeString([], {
              hour: "2-digit",
              minute: "2-digit",
            })}
          </p>
        </div>
        {isOwnMessage && (
          <Button
            variant="ghost"
            size="icon"
            onClick={onDelete}
            className="opacity-0 hover:opacity-100 transition-opacity"
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        )}
      </div>
    </div>
  );
};

export default MessageBubble;
