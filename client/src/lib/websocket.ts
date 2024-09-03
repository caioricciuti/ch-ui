// websocket.ts

import { io, Socket } from "socket.io-client";

const SOCKET_SERVER_URL = "http://localhost:5124";

class WebSocketManager {
  private static instance: WebSocketManager;
  private socket: Socket | null = null;

  private constructor() {
    this.connectSocket();
  }

  private connectSocket() {
    this.socket = io(SOCKET_SERVER_URL, {
      withCredentials: true,
      autoConnect: true,
    });

    this.socket.on("connect", () => {
      console.log("Connected to WebSocket server");
    });

    this.socket.on("disconnect", () => {
      console.log("Disconnected from WebSocket server");
    });

    this.socket.on("connect_error", (error) => {
      console.error("Connection error:", error.message);
    });

    this.socket.on("new_message", (data) => {
      console.log("New message received:", data);
    });

    this.socket.on("message_deleted", (data) => {
      console.log("Message deleted:", data);
    });

    this.socket.on("chat_deleted", (data) => {
      console.log("Chat deleted:", data);
    });
  }

  public static getInstance(): WebSocketManager {
    if (!WebSocketManager.instance) {
      WebSocketManager.instance = new WebSocketManager();
    }
    return WebSocketManager.instance;
  }

  public getSocket(): Socket | null {
    return this.socket;
  }

  public on(event: string, callback: (...args: any[]) => void) {
    this.socket?.on(event, callback);
  }

  public emit(event: string, data: any) {
    this.socket?.emit(event, data);
  }

  public reconnect() {
    if (this.socket) {
      this.socket.disconnect();
      this.connectSocket();
    }
  }

  public joinChatRoom(chatId: string) {
    if (this.socket) {
      this.socket.emit("join_chat", chatId);
    } else {
      console.error("Socket is not initialized");
    }
  }

  public leaveChatRoom(chatId: string) {
    if (this.socket) {
      this.socket.emit("leave_chat", chatId);
    } else {
      console.error("Socket is not initialized");
    }
  }

  public sendMessage(chatId: string, message: string) {
    if (this.socket) {
      this.socket.emit("send_message", { chatId, message });
    } else {
      console.error("Socket is not initialized");
    }
  }
}

export default WebSocketManager;
