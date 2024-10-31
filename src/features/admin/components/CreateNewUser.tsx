import React from "react";
import { useForm } from "react-hook-form";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { Calendar } from "@/components/ui/calendar";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { CalendarIcon, Plus, X, Key, Eye, EyeOff } from "lucide-react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import { format } from "date-fns";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import useAppStore from "@/store";
import { generateRandomPassword } from "@/lib/utils";

const CreateNewUser = ({ onUserCreated }: { onUserCreated: () => void }) => {
  const { runQuery } = useAppStore();

  const [isOpen, setIsOpen] = React.useState(false);
  const [error, setError] = React.useState("");
  const [loading, setLoading] = React.useState(false);
  const [roles, setRoles] = React.useState([]);
  const [databases, setDatabases] = React.useState([]);
  const [profiles, setProfiles] = React.useState([]);
  const [showPassword, setShowPassword] = React.useState(false);

  const form = useForm({
    defaultValues: {
      username: "",
      password: "",
      hostType: "ANY",
      hostValue: "",
      validUntil: undefined,
      defaultRole: "",
      defaultDatabase: "",
      grantDatabases: [],
      grantees: "NONE",
      settings: {
        profile: "",
        readonly: false,
      },
      privileges: {
        isAdmin: false,
        allowDDL: false,
        allowInsert: false,
        allowSelect: false,
        allowAlter: false,
        allowCreate: false,
        allowDrop: false,
        allowTruncate: false,
      },
    },
  });

  React.useEffect(() => {
    const fetchMetadata = async () => {
      try {
        const rolesResult = await runQuery("SHOW ROLES");
        if (!rolesResult.error && rolesResult.data) {
          setRoles(rolesResult.data.map((row: any) => row.name));
        }

        const dbResult = await runQuery("SHOW DATABASES");
        if (!dbResult.error && dbResult.data) {
          setDatabases(dbResult.data.map((row: any) => row.name));
        }

        const profilesResult = await runQuery("SHOW SETTINGS PROFILES");
        if (!profilesResult.error && profilesResult.data) {
          setProfiles(profilesResult.data.map((row: any) => row.name));
        }
      } catch (err) {
        console.error("Failed to fetch metadata:", err);
      }
    };

    if (isOpen) {
      fetchMetadata();
    }
  }, [isOpen, runQuery]);

  const handleGeneratePassword = () => {
    const newPassword = generateRandomPassword();
    form.setValue("password", newPassword);
  };

  const buildUserCreationQuery = (data: any) => {
    let query = `CREATE USER IF NOT EXISTS ${data.username}`;

    // Add authentication
    query += ` IDENTIFIED WITH sha256_password BY '${data.password}'`;

    // Add host restrictions if specified
    if (data.hostType !== "ANY") {
      query += ` HOST ${data.hostType} '${data.hostValue}'`;
    }

    // Add validity period if specified
    if (data.validUntil) {
      query += ` VALID UNTIL '${format(
        data.validUntil,
        "yyyy-MM-dd HH:mm:ss"
      )}'`;
    }

    // Add default role if specified
    if (data.defaultRole) {
      query += ` DEFAULT ROLE ${data.defaultRole}`;
    }

    // Add default database if specified
    if (data.defaultDatabase) {
      query += ` DEFAULT DATABASE ${data.defaultDatabase}`;
    }

    // Add grantees setting
    query += ` GRANTEES ${data.grantees}`;

    // Add settings profile and readonly mode if specified
    if (data.settings.profile) {
      query += ` SETTINGS PROFILE '${data.settings.profile}'`;
    }
    if (data.settings.readonly) {
      query += ` SETTINGS READONLY=1`;
    }

    return query;
  };

  const buildGrantQueries = (username: string, data: any) => {
    const queries = [];

    if (data.privileges.isAdmin) {
      queries.push(`GRANT ALL ON *.* TO ${username} WITH GRANT OPTION`);
      return queries;
    }

    for (const db of data.grantDatabases) {
      const privileges = [];

      if (data.privileges.allowSelect) privileges.push("SELECT");
      if (data.privileges.allowInsert) privileges.push("INSERT");
      if (data.privileges.allowAlter) privileges.push("ALTER");
      if (data.privileges.allowCreate) privileges.push("CREATE");
      if (data.privileges.allowDrop) privileges.push("DROP");
      if (data.privileges.allowTruncate) privileges.push("TRUNCATE");

      if (privileges.length > 0) {
        queries.push(
          `GRANT ${privileges.join(", ")} ON ${db}.* TO ${username}`
        );
      }

      // If DDL is allowed, grant additional schema modification privileges
      if (data.privileges.allowDDL) {
        queries.push(
          `GRANT CREATE, DROP, ALTER, CREATE DATABASE ON ${db}.* TO ${username}`
        );
      }
    }

    return queries;
  };

  const onSubmit = async (data: any) => {
    try {
      setError("");
      setLoading(true);

      // Validate database grants
      if (!data.privileges.isAdmin && data.grantDatabases.length === 0) {
        setError("Please select at least one database to grant access to");
        return;
      }

      // Create user
      const createUserQuery = buildUserCreationQuery(data);
      const createResult = await runQuery(createUserQuery);

      if (createResult.error) {
        setError(createResult.error);
        return;
      }

      // Grant privileges
      const grantQueries = buildGrantQueries(data.username, data);

      for (const query of grantQueries) {
        const result = await runQuery(query);
        if (result.error) {
          // If there's an error, try to clean up by dropping the user
          await runQuery(`DROP USER IF EXISTS ${data.username}`);
          setError(`Failed to grant privileges: ${result.error}`);
          return;
        }
      }

      // If readonly mode is enabled, add additional restrictions
      if (data.settings.readonly) {
        await runQuery(`ALTER USER ${data.username} SETTINGS READONLY=1`);
      }

      onUserCreated();
      setIsOpen(false);
      form.reset();
    } catch (err: any) {
      setError(err.message || "Failed to create user");
    } finally {
      setLoading(false);
    }
  };

  return (
    <Sheet open={isOpen} onOpenChange={setIsOpen}>
      <SheetTrigger asChild>
        <Button className="gap-2 max-w-fit" variant="outline">
          <Plus className="h-4 w-4" />
          Create New User
        </Button>
      </SheetTrigger>
      <SheetContent className="sm:max-w-xl overflow-y-auto">
        <SheetHeader>
          <SheetTitle>Create New ClickHouse User</SheetTitle>
        </SheetHeader>

        <Form {...form}>
          <form
            onSubmit={form.handleSubmit(onSubmit)}
            className="space-y-4 pt-6"
          >
            {/* Authentication */}
            <Card>
              <CardHeader>
                <CardTitle>Authentication</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <FormField
                  control={form.control}
                  name="username"
                  rules={{
                    required: "Username is required",
                    minLength: {
                      value: 3,
                      message: "Username must be at least 3 characters",
                    },
                    pattern: {
                      value: /^[a-zA-Z0-9_]+$/,
                      message: "Only letters, numbers, and underscores allowed",
                    },
                  }}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>Username</FormLabel>
                      <FormControl>
                        <Input placeholder="Enter username" {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name="password"
                  rules={{
                    required: "Password is required",
                    validate: (value) => {
                      if (value.length < 12) {
                        return "Password must be at least 12 characters";
                      }
                      if (
                        !/[a-z]/.test(value) ||
                        !/[A-Z]/.test(value) ||
                        !/\d/.test(value) ||
                        !/[!@#$%^&*]/.test(value)
                      ) {
                        return "Password must include uppercase, lowercase, numbers, and special characters";
                      }
                      return true;
                    },
                  }}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>Password</FormLabel>
                      <div className="flex space-x-2 w-full">
                        <FormControl>
                          <div className="relative flex-1">
                            <Input
                              type={showPassword ? "text" : "password"}
                              placeholder="Enter password"
                              className="w-[100%]"
                              {...field}
                            />
                            <Button
                              type="button"
                              variant="ghost"
                              size="sm"
                              className="absolute right-2 top-0 h-full px-3"
                              onClick={() => setShowPassword(!showPassword)}
                            >
                              {showPassword ? (
                                <EyeOff className="h-4 w-4" />
                              ) : (
                                <Eye className="h-4 w-4" />
                              )}
                            </Button>
                          </div>
                        </FormControl>
                        <Button
                          type="button"
                          onClick={handleGeneratePassword}
                          variant="outline"
                        >
                          <Key className="h-4 w-4 md:mr-2" />
                          <span className="hidden md:block">Generate</span>
                        </Button>
                      </div>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </CardContent>
            </Card>

            {/* Access Control */}
            <Card>
              <CardHeader>
                <CardTitle>Access Control</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <FormField
                  control={form.control}
                  name="hostType"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>Host Type</FormLabel>
                      <Select
                        onValueChange={field.onChange}
                        defaultValue={field.value}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue placeholder="Select host type" />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          <SelectItem value="ANY">Any Host</SelectItem>
                          <SelectItem value="LOCAL">Local Only</SelectItem>
                          <SelectItem value="IP">Specific IP</SelectItem>
                          <SelectItem value="NAME">Host Name</SelectItem>
                          <SelectItem value="REGEXP">
                            Regular Expression
                          </SelectItem>
                          <SelectItem value="LIKE">Like Pattern</SelectItem>
                        </SelectContent>
                      </Select>
                    </FormItem>
                  )}
                />

                {form.watch("hostType") !== "ANY" &&
                  form.watch("hostType") !== "LOCAL" && (
                    <FormField
                      control={form.control}
                      name="hostValue"
                      rules={{ required: "Host value is required" }}
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>Host Value</FormLabel>
                          <FormControl>
                            <Input placeholder="Enter host value" {...field} />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  )}

                <FormField
                  control={form.control}
                  name="validUntil"
                  render={({ field }) => (
                    <FormItem className="flex flex-col">
                      <FormLabel>Valid Until</FormLabel>
                      <Popover>
                        <PopoverTrigger asChild>
                          <FormControl>
                            <Button
                              variant="outline"
                              className={cn(
                                "w-full pl-3 text-left font-normal",
                                !field.value && "text-muted-foreground"
                              )}
                            >
                              {field.value
                                ? format(field.value, "PPP")
                                : "Pick a date"}
                              <CalendarIcon className="ml-auto h-4 w-4 opacity-50" />
                            </Button>
                          </FormControl>
                        </PopoverTrigger>
                        <PopoverContent className="w-auto p-0" align="start">
                          <Calendar
                            className="rounded-md border"
                            classNames=""
                            mode="single"
                            selected={field.value}
                            onSelect={field.onChange}
                            initialFocus
                          />
                        </PopoverContent>
                      </Popover>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </CardContent>
            </Card>

            {/* Database and Roles */}
            <Card>
              <CardHeader>
                <CardTitle>Database and Roles</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <FormField
                  control={form.control}
                  name="defaultRole"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>Default Role</FormLabel>
                      <Select
                        onValueChange={field.onChange}
                        defaultValue={field.value}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue placeholder="Select default role" />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          {roles.map((role) => (
                            <SelectItem key={role} value={role}>
                              {role}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name="defaultDatabase"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>Default Database</FormLabel>
                      <Select
                        onValueChange={field.onChange}
                        value={field.value}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue placeholder="Select default database" />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          {databases.map((db) => (
                            <SelectItem key={db} value={db}>
                              {db}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name="grantDatabases"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>Grant Access to Databases</FormLabel>
                      <Select
                        onValueChange={(value) =>
                          field.onChange([...field.value, value])
                        }
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue placeholder="Select databases to grant access" />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          {databases.map((db) => (
                            <SelectItem
                              key={db}
                              value={db}
                              disabled={field.value.includes(db)}
                            >
                              {db}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <div className="flex flex-wrap gap-2 mt-2">
                        {field.value.map((db) => (
                          <Badge
                            key={db}
                            variant="secondary"
                            className="hover:bg-destructive hover:text-destructive-foreground cursor-pointer"
                            onClick={() =>
                              field.onChange(
                                field.value.filter((v) => v !== db)
                              )
                            }
                          >
                            {db}
                            <X className="ml-1 h-3 w-3" />
                          </Badge>
                        ))}
                      </div>
                    </FormItem>
                  )}
                />
              </CardContent>
            </Card>

            {/* Privileges */}
            <Card>
              <CardHeader>
                <CardTitle>Privileges</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <FormField
                  control={form.control}
                  name="privileges.isAdmin"
                  render={({ field }) => (
                    <FormItem className="flex flex-row items-start space-x-3 space-y-0 rounded-md border p-4">
                      <FormControl>
                        <Checkbox
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                      <div className="space-y-1 leading-none">
                        <FormLabel>Admin Privileges</FormLabel>
                        <FormDescription>
                          Grant all privileges (INCLUDING GRANT OPTION) on all
                          databases
                        </FormDescription>
                      </div>
                    </FormItem>
                  )}
                />

                {!form.watch("privileges.isAdmin") && (
                  <>
                    <FormField
                      control={form.control}
                      name="privileges.allowSelect"
                      render={({ field }) => (
                        <FormItem className="flex flex-row items-start space-x-3 space-y-0 rounded-md border p-4">
                          <FormControl>
                            <Checkbox
                              checked={field.value}
                              onCheckedChange={field.onChange}
                            />
                          </FormControl>
                          <div className="space-y-1 leading-none">
                            <FormLabel>Select Permission</FormLabel>
                            <FormDescription>
                              Allow reading data from tables
                            </FormDescription>
                          </div>
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name="privileges.allowInsert"
                      render={({ field }) => (
                        <FormItem className="flex flex-row items-start space-x-3 space-y-0 rounded-md border p-4">
                          <FormControl>
                            <Checkbox
                              checked={field.value}
                              onCheckedChange={field.onChange}
                            />
                          </FormControl>
                          <div className="space-y-1 leading-none">
                            <FormLabel>Insert Permission</FormLabel>
                            <FormDescription>
                              Allow inserting data into tables
                            </FormDescription>
                          </div>
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name="privileges.allowDDL"
                      render={({ field }) => (
                        <FormItem className="flex flex-row items-start space-x-3 space-y-0 rounded-md border p-4">
                          <FormControl>
                            <Checkbox
                              checked={field.value}
                              onCheckedChange={field.onChange}
                            />
                          </FormControl>
                          <div className="space-y-1 leading-none">
                            <FormLabel>DDL Permission</FormLabel>
                            <FormDescription>
                              Allow creating, dropping, and altering tables
                            </FormDescription>
                          </div>
                        </FormItem>
                      )}
                    />
                  </>
                )}
              </CardContent>
            </Card>

            {/* Settings */}
            <Card>
              <CardHeader>
                <CardTitle>Settings</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <FormField
                  control={form.control}
                  name="settings.profile"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>Settings Profile</FormLabel>
                      <Select
                        onValueChange={field.onChange}
                        defaultValue={field.value}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue placeholder="Select settings profile" />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          {profiles.map((profile) => (
                            <SelectItem key={profile} value={profile}>
                              {profile}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name="settings.readonly"
                  render={({ field }) => (
                    <FormItem className="flex flex-row items-start space-x-3 space-y-0 rounded-md border p-4">
                      <FormControl>
                        <Checkbox
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                      <div className="space-y-1 leading-none">
                        <FormLabel>Read-only Mode</FormLabel>
                        <FormDescription>
                          Restrict user to read-only operations
                        </FormDescription>
                      </div>
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name="grantees"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>Grantees</FormLabel>
                      <Select
                        onValueChange={field.onChange}
                        defaultValue={field.value}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue placeholder="Select grantees" />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          <SelectItem value="ANY">Any</SelectItem>
                          <SelectItem value="NONE">None</SelectItem>
                        </SelectContent>
                      </Select>
                      <FormDescription>
                        Specify if this user can grant their privileges to
                        others
                      </FormDescription>
                    </FormItem>
                  )}
                />
                {error && (
                  <Alert variant="destructive">
                    <AlertDescription>{error}</AlertDescription>
                  </Alert>
                )}
              </CardContent>
            </Card>

            <Button type="submit" className="w-full" disabled={loading}>
              {loading ? "Creating..." : "Create User"}
            </Button>
          </form>
        </Form>
      </SheetContent>
    </Sheet>
  );
};

export default CreateNewUser;
