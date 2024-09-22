import React, { useState, useEffect } from "react";
import {
  Bell,
  Info,
  AlertTriangle,
  CheckCircle,
  MailCheckIcon,
  Trash2,
  MoreVertical,
  Loader2,
} from "lucide-react";
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import api from "@/api/axios.config";
import { Skeleton } from "@/components/ui/skeleton";

type NotificationType = "info" | "warning" | "success";

type Notification = {
  _id: string;
  userId: string;
  title: string;
  message: string;
  type: NotificationType;
  read: boolean;
  createdAt: string;
};

const typeIcons: Record<NotificationType, React.ReactNode> = {
  info: <Info className="h-4 w-4" />,
  warning: <AlertTriangle className="h-4 w-4" />,
  success: <CheckCircle className="h-4 w-4" />,
};

const typeColors: Record<NotificationType, string> = {
  info: "bg-blue-100/15 text-blue-500 border-blue-500",
  warning: "bg-yellow-300/15 text-yellow-600 border-yellow-500",
  success: "bg-green-300/15 text-green-500 border-green-500",
};

const NotificationCard: React.FC<{
  notification: Notification;
  onMarkAsRead: (id: string) => void;
  onDelete: (id: string) => void;
}> = ({ notification, onMarkAsRead, onDelete }) => (
  <ContextMenu>
    <ContextMenuTrigger>
      <Card className={`mb-4 overflow-hidden ${typeColors[notification.type]}`}>
        <CardHeader className="pb-2">
          <CardTitle className="text-lg font-semibold flex items-center justify-between">
            <span className="flex items-center">
              {typeIcons[notification.type]}
              <span className="ml-2">{notification.title}</span>
            </span>
            <Badge variant="outline" className={typeColors[notification.type]}>
              {notification.type}
            </Badge>
          </CardTitle>
        </CardHeader>
        <CardContent className="pb-1">
          <p className="text-sm">{notification.message}</p>
        </CardContent>
        <CardFooter className="flex justify-between items-center pt-2">
          <span className="text-xs">
            {new Date(notification.createdAt).toLocaleString()}
          </span>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" className="h-8 w-8 p-0">
                <MoreVertical className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              {!notification.read && (
                <DropdownMenuItem
                  onClick={() => onMarkAsRead(notification._id)}
                >
                  <MailCheckIcon className="mr-2 h-4 w-4" />
                  Mark as Read
                </DropdownMenuItem>
              )}
              <DropdownMenuItem onClick={() => onDelete(notification._id)}>
                <Trash2 className="mr-2 h-4 w-4" />
                Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </CardFooter>
      </Card>
    </ContextMenuTrigger>
    <ContextMenuContent>
      {!notification.read && (
        <ContextMenuItem onClick={() => onMarkAsRead(notification._id)}>
          Mark as Read
        </ContextMenuItem>
      )}
      <ContextMenuItem onClick={() => onDelete(notification._id)}>
        Delete
      </ContextMenuItem>
    </ContextMenuContent>
  </ContextMenu>
);

const NotificationsPage: React.FC = () => {
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [activeTab, setActiveTab] = useState<"unread" | "read">("unread");
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetchNotifications();
  }, []);

  const fetchNotifications = async () => {
    setIsLoading(true);
    setError(null);
    try {
      const response = await api.get("notifications/");
      setNotifications(response.data.data || []);
    } catch (error) {
      console.error("Error fetching notifications:", error);
      setError("Failed to fetch notifications. Please try again later.");
    } finally {
      setIsLoading(false);
    }
  };

  const markAsRead = async (id: string) => {
    try {
      await api.put(`notifications/read`, { id });
      setNotifications(
        notifications.map((notif) =>
          notif._id === id ? { ...notif, read: true } : notif
        )
      );
    } catch (error) {
      console.error("Error marking notification as read:", error);
      setError("Failed to mark notification as read. Please try again.");
    }
  };

  const markAllAsRead = async () => {
    try {
      await api.put("notifications/read-all");
      setNotifications(
        notifications.map((notif) => ({ ...notif, read: true }))
      );
    } catch (error) {
      console.error("Error marking all notifications as read:", error);
      setError("Failed to mark all notifications as read. Please try again.");
    }
  };

  const deleteNotification = async (id: string) => {
    try {
      await api.delete("notifications/delete", { data: { id } });
      setNotifications(notifications.filter((notif) => notif._id !== id));
    } catch (error) {
      console.error("Error deleting notification:", error);
      setError("Failed to delete notification. Please try again.");
    }
  };

  const deleteAllNotifications = async () => {
    try {
      await api.delete("notifications/delete-all");
      setNotifications([]);
    } catch (error) {
      console.error("Error deleting all notifications:", error);
      setError("Failed to delete all notifications. Please try again.");
    }
  };

  if (isLoading) {
    return (
      <>
        <div className="text-center py-4 w-52 m-auto items-center flex space-x-3">
          <Loader2 size={32} className="animate-spin" />
          <p>Loading Notifications</p>
        </div>
        <div className="container mx-auto p-4 max-w-3xl">
          <Skeleton className="h-20 w-full mb-4" />
          <Skeleton className="h-20 w-full mb-4" />
          <Skeleton className="h-20 w-full mb-4" />
          <Skeleton
            className="h-20 w-full mb-4"
            style={{ borderRadius: "0.5rem" }}
          />
        </div>
      </>
    );
  }

  const unreadNotifications = notifications.filter((n) => !n.read);
  const readNotifications = notifications.filter((n) => n.read);

  return (
    <div className="container mx-auto p-4 max-w-4xl">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-3xl font-bold flex items-center">
          <Bell className="mr-2" /> Notifications
        </h1>
      </div>

      {error && (
        <Alert variant="destructive" className="mb-4">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      <Tabs
        value={activeTab}
        onValueChange={(value) => setActiveTab(value as "unread" | "read")}
      >
        <TabsList className="grid w-full grid-cols-2 mb-4">
          <TabsTrigger value="unread">
            Unread ({unreadNotifications.length})
          </TabsTrigger>
          <TabsTrigger value="read">
            Read ({readNotifications.length})
          </TabsTrigger>
        </TabsList>

        <div className="flex justify-end space-x-2 mb-4">
          <Button
            onClick={markAllAsRead}
            disabled={unreadNotifications.length === 0}
            size="sm"
            variant="ghost"
          >
            Mark All as Read
          </Button>
          <Button
            onClick={deleteAllNotifications}
            variant="destructive"
            disabled={notifications.length === 0}
            size="sm"
          >
            Delete All
          </Button>
        </div>

        <TabsContent value="unread">
          <ScrollArea className="h-[70vh] pr-4">
            {unreadNotifications.length === 0 ? (
              <p className="text-center text-gray-500">
                No unread notifications.
              </p>
            ) : (
              unreadNotifications.map((notification) => (
                <NotificationCard
                  key={notification._id}
                  notification={notification}
                  onMarkAsRead={markAsRead}
                  onDelete={deleteNotification}
                />
              ))
            )}
          </ScrollArea>
        </TabsContent>

        <TabsContent value="read">
          <ScrollArea className="h-[70vh] pr-4">
            {readNotifications.length === 0 ? (
              <p className="text-center text-gray-500">
                No read notifications.
              </p>
            ) : (
              readNotifications.map((notification) => (
                <NotificationCard
                  key={notification._id}
                  notification={notification}
                  onMarkAsRead={markAsRead}
                  onDelete={deleteNotification}
                />
              ))
            )}
          </ScrollArea>
        </TabsContent>
      </Tabs>
    </div>
  );
};

export default NotificationsPage;
