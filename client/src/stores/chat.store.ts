import { create } from "zustand";
import { ChatState, Message } from "@/types/types";
import api from "@/api/axios.config";
import { io } from "socket.io-client";

const socket = io("http://localhost:5124", { withCredentials: true });

const useChatStore = create<ChatState>((set) => ({
  chats: [],
  selectedChat: null,
  isLoading: false,
  error: null,

  fetchChats: async () => {
    set({ isLoading: true, error: null });
    try {
      const response = await api.get("chats/");
      set({ chats: response.data, isLoading: false });
    } catch (error) {
      set({ error: "Failed to fetch chats", isLoading: false });
    }
  },

  createChat: async (participantId: string) => {
    set({ isLoading: true, error: null });
    try {
      const response = await api.post("chats/", { participantId });
      const newChat = response.data;
      set((state) => ({
        chats: [newChat, ...state.chats],
        selectedChat: newChat,
        isLoading: false,
      }));
      return newChat;
    } catch (error) {
      set({ error: "Failed to create chat", isLoading: false });
      throw error;
    }
  },

  fetchChat: async (chatId: string) => {
    set({ isLoading: true, error: null });
    try {
      const response = await api.get(`chats/${chatId}`);
      set({ selectedChat: response.data, isLoading: false });
      socket.emit("join_chat", chatId);
    } catch (error) {
      set({ error: "Failed to fetch chat", isLoading: false });
    }
  },

  sendMessage: async (chatId: string, content: string) => {
    try {
      const response = await api.post(`chats/${chatId}/messages`, { content });
      const newMessage = response.data;
      set((state) => ({
        chats: state.chats.map((chat) =>
          chat._id === chatId
            ? {
                ...chat,
                messages: [...(chat.messages || []), newMessage],
                updatedAt: new Date(),
              }
            : chat
        ),
        selectedChat:
          state.selectedChat?._id === chatId
            ? {
                ...state.selectedChat,
                messages: [...(state.selectedChat.messages || []), newMessage],
              }
            : state.selectedChat,
      }));
      return newMessage;
    } catch (error) {
      console.error("Failed to send message", error);
      throw error;
    }
  },

  deleteMessage: async (chatId: string, messageId: string) => {
    try {
      await api.delete(`chats/${chatId}/messages/${messageId}`);
      set((state) => ({
        chats: state.chats.map((chat) =>
          chat._id === chatId
            ? {
                ...chat,
                messages: chat.messages.filter((msg) => msg._id !== messageId),
              }
            : chat
        ),
        selectedChat:
          state.selectedChat?._id === chatId
            ? {
                ...state.selectedChat,
                messages: state.selectedChat.messages.filter(
                  (msg) => msg._id !== messageId
                ),
              }
            : state.selectedChat,
      }));
    } catch (error) {
      console.error("Failed to delete message", error);
      throw error;
    }
  },

  deleteChat: async (chatId: string) => {
    try {
      await api.delete(`chats/${chatId}`);
      set((state) => ({
        chats: state.chats.filter((chat) => chat._id !== chatId),
        selectedChat:
          state.selectedChat?._id === chatId ? null : state.selectedChat,
      }));
    } catch (error) {
      console.error("Failed to delete chat", error);
      throw error;
    }
  },

  handleNewMessage: (chatId: string, message: Message) => {
    set((state) => ({
      chats: state.chats.map((chat) =>
        chat._id === chatId
          ? {
              ...chat,
              messages: [...(chat.messages || []), message],
              updatedAt: new Date(),
            }
          : chat
      ),
      selectedChat:
        state.selectedChat?._id === chatId
          ? {
              ...state.selectedChat,
              messages: [...(state.selectedChat.messages || []), message],
            }
          : state.selectedChat,
    }));
  },

  handleMessageDeleted: (chatId: string, messageId: string) => {
    set((state) => ({
      chats: state.chats.map((chat) =>
        chat._id === chatId
          ? {
              ...chat,
              messages: chat.messages.filter((msg) => msg._id !== messageId),
            }
          : chat
      ),
      selectedChat:
        state.selectedChat?._id === chatId
          ? {
              ...state.selectedChat,
              messages: state.selectedChat.messages.filter(
                (msg) => msg._id !== messageId
              ),
            }
          : state.selectedChat,
    }));
  },

  handleChatDeleted: (chatId: string) => {
    set((state) => ({
      chats: state.chats.filter((chat) => chat._id !== chatId),
      selectedChat:
        state.selectedChat?._id === chatId ? null : state.selectedChat,
    }));
  },
}));

// Listen to Socket.io events
socket.on("new_message", (message) => {
  useChatStore.getState().handleNewMessage(message.chatId, message);
});

socket.on("message_deleted", ({ chatId, messageId }) => {
  useChatStore.getState().handleMessageDeleted(chatId, messageId);
});

socket.on("chat_deleted", ({ chatId }) => {
  useChatStore.getState().handleChatDeleted(chatId);
});

export default useChatStore;
