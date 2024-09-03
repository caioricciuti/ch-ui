import React, { useEffect, useCallback } from "react";
import useAuthStore from "@/stores/user.store";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import api from "@/api/axios.config";
import { useNavigate } from "react-router-dom";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Search } from "lucide-react";
import { toast } from "sonner";
import { getInitials, bgColorsByInitials } from "@/lib/helpers";

interface NewChatComponentProps {
  isOpen: boolean;

  onClose: () => void;

  fetchChats: () => Promise<void>;
}
const NewChatComponent: React.FC<NewChatComponentProps> = ({
  isOpen,
  onClose,
  fetchChats,
}) => {
  const { allUsers, getAllUsers, user: currentUser } = useAuthStore();
  const navigate = useNavigate();
  const [searchTerm, setSearchTerm] = React.useState("");

  // Ensure that getAllUsers is stable
  const fetchAllUsers = useCallback(() => {
    getAllUsers();
  }, [getAllUsers]);

  useEffect(() => {
    fetchAllUsers();
  }, [fetchAllUsers]);

  const startNewChat = async (userId: string) => {
    try {
      const response = await api.post("chats/", { participantId: userId });
      const newChat = response.data;
      toast.success("Chat created successfully");
      navigate(`/chats/${newChat._id}`);
      onClose();
      await fetchChats();
    } catch (error: any) {
      // Close the dialog and show an error toast
      onClose();
      navigate(`/chats/${error.response.data.error.chatId}`);
    }
  };

  const filteredUsers = allUsers.filter(
    (user) =>
      user._id !== currentUser?._id &&
      user.name.toLowerCase().includes(searchTerm.toLowerCase())
  );

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>Start a New Chat</DialogTitle>
        </DialogHeader>
        <div className="relative">
          <Input
            placeholder="Search users..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="pl-8"
          />
          <Search className="absolute left-2 top-1/2 transform -translate-y-1/2 h-4 w-4 text-gray-500" />
        </div>
        <ScrollArea className="mt-2 max-h-[300px] pr-4">
          {filteredUsers.length > 0 ? (
            filteredUsers.map((user) => (
              <Button
                key={user._id}
                variant="ghost"
                className="w-full justify-start mb-1"
                onClick={() => startNewChat(user._id)}
              >
                <Avatar className="w-8 h-8 mr-2">
                  <AvatarFallback
                    className={`font-bold
                ${bgColorsByInitials(getInitials(user.name || ""))}
                `}
                  >
                    {getInitials(user.name || "")}
                  </AvatarFallback>
                </Avatar>
                <span className="text-sm font-medium">{user.name}</span>
              </Button>
            ))
          ) : (
            <p className="text-center text-gray-500 mt-4">No users found</p>
          )}
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
};

export default NewChatComponent;
