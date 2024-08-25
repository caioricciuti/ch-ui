import { useState } from "react";
import { useForm } from "react-hook-form";
import useAuthStore from "@/stores/user.store";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { toast } from "sonner";
import { ModeToggle } from "@/components/mode-toggle";
import { getInitials, bgColorsByInitials } from "@/lib/helpers";
import { Pencil } from "lucide-react";

function SettingsPage() {
  const { user, updateUser } = useAuthStore();
  const [isEditing, setIsEditing] = useState(false);

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm({
    defaultValues: {
      name: user?.name || "",
    },
  });

  const onSubmit = async (data: any) => {
    try {
      if (user) {
        console.log(user._id, data);
        await updateUser(user._id, data);
        toast.success("Profile updated successfully");
        setIsEditing(false);
      }
    } catch (error: any) {
      console.error("Update failed:", error);
      toast.error(`Update failed: ${error.message}`);
    }
  };

  return (
    <div className="container mx-auto p-4">
      <h1 className="text-3xl font-bold mb-6">Settings</h1>

      <Tabs defaultValue="account">
        <TabsList className="mb-4">
          <TabsTrigger value="account">Account</TabsTrigger>
          <TabsTrigger value="security">Security</TabsTrigger>
          <TabsTrigger value="theme">Theming</TabsTrigger>
        </TabsList>

        <TabsContent value="account">
          <Card>
            <CardHeader>
              <CardTitle>Account Information</CardTitle>
              <CardDescription>
                Manage your account details here.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="flex items-center space-x-4 mb-4">
                <Avatar className="h-20 w-20">
                  <AvatarFallback
                    className={`text-3xl font-bold
                ${bgColorsByInitials(getInitials(user?.name || ""))}
                `}
                  >
                    {getInitials(user?.name || "")}
                  </AvatarFallback>
                </Avatar>
                <div>
                  <div className="flex items-center space-x-2">
                    <h2 className="text-2xl font-bold">{user?.name}</h2>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => setIsEditing(true)}
                      className="p-1"
                    >
                      <Pencil className="h-4 w-4 text-gray-500" />
                    </Button>
                  </div>
                  <div>
                    <p className="text-gray-500 flex items-center">
                      {user?._id}
                    </p>
                    <p className="text-gray-500 flex items-center">
                      {user?.email}
                    </p>
                    <p className="text-sm text-gray-500 flex items-center capitalize mt-1">
                      Role:{" "}
                      <span
                        className={`ml-2 px-1.5  py-.5 rounded-full font-bold ${
                          user?.role === "admin"
                            ? "bg-orange-500/50 text-white"
                            : "bg-green-500/50 text-white"
                        }`}
                      >
                        {user?.role}
                      </span>
                    </p>
                  </div>
                </div>
              </div>

              {isEditing && (
                <form onSubmit={handleSubmit(onSubmit)}>
                  <div className="space-y-4">
                    <div>
                      <Label htmlFor="name">Name</Label>
                      <Input
                        id="name"
                        {...register("name", { required: "Name is required" })}
                      />
                      {errors.name && (
                        <p className="text-red-500 text-sm">
                          {errors.name.message}
                        </p>
                      )}
                    </div>
                  </div>
                  <div className="mt-4 space-x-2">
                    <Button type="submit">Save Changes</Button>
                    <Button
                      variant="outline"
                      onClick={() => setIsEditing(false)}
                    >
                      Cancel
                    </Button>
                  </div>
                </form>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="security">
          <Card>
            <CardHeader>
              <CardTitle>User Security</CardTitle>
              <CardDescription>
                Manage your security settings here.
              </CardDescription>
            </CardHeader>
            <CardContent></CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="theme">
          <Card>
            <CardHeader>
              <CardTitle>Theming</CardTitle>
              <CardDescription>Change your theme settings.</CardDescription>
            </CardHeader>
            <CardContent className="flex items-start justify-start">
              <ModeToggle />
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}

export default SettingsPage;
