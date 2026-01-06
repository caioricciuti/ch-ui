import { useEffect } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import * as z from "zod";
import { Loader2, Database, ChevronRight, Server } from "lucide-react";
import { useNavigate } from "react-router-dom";
import useAppStore from "@/store";
import { motion } from "framer-motion";

import {
    Form,
    FormControl,
    FormDescription,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
    Card,
    CardContent,
    CardDescription,
    CardFooter,
    CardHeader,
    CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { withBasePath } from "@/lib/basePath";

// Schema for the login form
const loginSchema = z.object({
    url: z.string().min(1, "ClickHouse URL is required"),
    username: z.string().min(1, "Username is required"),
    password: z.string().optional(),

});

const Logo = withBasePath("logo.svg");

export default function Login() {
    const navigate = useNavigate();
    const { setCredential, isLoadingCredentials, error, isServerAvailable, clearCredentials } =
        useAppStore();

    // Get defaults from environment
    const envUrls = window.env?.VITE_CLICKHOUSE_URLS || [];



    const availableUrls = envUrls;
    const defaultUrl = availableUrls.length > 0 ? availableUrls[0] : "";

    const form = useForm<z.infer<typeof loginSchema>>({
        resolver: zodResolver(loginSchema) as any,
        defaultValues: {
            url: defaultUrl,
            username: "default",
            password: "",
        },
    });

    const onSubmit = async (values: z.infer<typeof loginSchema>) => {
        await clearCredentials();
        await setCredential({
            url: values.url,
            username: values.username,
            password: values.password || "",
        });
    };

    useEffect(() => {
        if (isServerAvailable) {
            navigate("/");
        }
    }, [isServerAvailable, navigate]);



    return (
        <div className="flex min-h-screen w-full items-center justify-center bg-[#0a0a0a] bg-[radial-gradient(ellipse_80%_80%_at_50%_-20%,rgba(120,119,198,0.3),rgba(255,255,255,0))] px-4 overflow-hidden relative">

            {/* Background Decor */}
            <div className="absolute inset-0 z-0">
                <div className="absolute top-1/4 left-1/4 w-96 h-96 bg-purple-500/10 rounded-full blur-3xl" />
                <div className="absolute bottom-1/4 right-1/4 w-96 h-96 bg-blue-500/10 rounded-full blur-3xl" />
            </div>

            <motion.div
                initial={{ opacity: 0, scale: 0.95, y: 20 }}
                animate={{ opacity: 1, scale: 1, y: 0 }}
                transition={{ duration: 0.5, ease: "easeOut" }}
                className="z-10 w-full max-w-md"
            >
                <Card className="w-full border-white/10 bg-black/40 backdrop-blur-xl shadow-2xl ring-1 ring-white/10">
                    <CardHeader className="space-y-3 flex flex-col items-center pb-8 pt-8">
                        <motion.div
                            initial={{ rotate: -10, opacity: 0 }}
                            animate={{ rotate: 0, opacity: 1 }}
                            transition={{ delay: 0.2, duration: 0.5 }}
                            className="w-20 h-20 flex items-center justify-center p-2"
                        >
                            <img src={Logo} alt="Logo" className="w-full h-full object-contain drop-shadow-[0_0_15px_rgba(255,200,0,0.3)]" />
                        </motion.div>
                        <div className="text-center space-y-1">
                            <CardTitle className="text-3xl font-bold tracking-tight bg-gradient-to-b from-white to-white/70 bg-clip-text text-transparent">
                                ClickHouse UI
                            </CardTitle>
                            <CardDescription className="text-base text-gray-400">
                                Authentication
                            </CardDescription>
                        </div>
                    </CardHeader>
                    <CardContent>
                        <Form {...form}>
                            <form onSubmit={form.handleSubmit(onSubmit as any)} className="space-y-5">

                                <FormField
                                    control={form.control as any}
                                    name="url"
                                    render={({ field }) => (
                                        <FormItem>
                                            <FormLabel className="text-gray-300">Server URL</FormLabel>
                                            {availableUrls.length > 0 ? (
                                                <Select onValueChange={field.onChange} defaultValue={field.value}>
                                                    <FormControl>
                                                        <SelectTrigger className="bg-white/5 border-white/10 text-white placeholder:text-gray-500 hover:bg-white/10 transition-colors h-11 overflow-hidden justify-start">
                                                            <div className="flex items-center gap-2 w-full overflow-x-auto whitespace-nowrap">
                                                                <Database className="w-4 h-4 text-purple-400 shrink-0" />
                                                                <SelectValue placeholder="Select a ClickHouse server" />
                                                            </div>
                                                        </SelectTrigger>
                                                    </FormControl>
                                                    <SelectContent className="bg-[#1a1a1a] border-white/10 text-white">
                                                        {availableUrls.map((url, idx) => (
                                                            <SelectItem key={`${url}-${idx}`} value={url} className="focus:bg-white/10 focus:text-white cursor-pointer">
                                                                {url}
                                                            </SelectItem>
                                                        ))}
                                                    </SelectContent>
                                                </Select>
                                            ) : (
                                                <FormControl>
                                                    <div className="relative">
                                                        <Server className="absolute left-3 top-3.5 w-4 h-4 text-gray-400" />
                                                        <Input placeholder="http://localhost:8123" {...field} className="pl-9 bg-white/5 border-white/10 text-white h-11" />
                                                    </div>
                                                </FormControl>
                                            )}
                                            <FormMessage />
                                        </FormItem>
                                    )}
                                />

                                <FormField
                                    control={form.control as any}
                                    name="username"
                                    render={({ field }) => (
                                        <FormItem>
                                            <FormLabel className="text-gray-300">Username</FormLabel>
                                            <FormControl>
                                                <Input placeholder="default" {...field} className="bg-white/5 border-white/10 text-white h-11" />
                                            </FormControl>
                                            <FormMessage />
                                        </FormItem>
                                    )}
                                />

                                <FormField
                                    control={form.control as any}
                                    name="password"
                                    render={({ field }) => (
                                        <FormItem>
                                            <FormLabel className="text-gray-300">Password</FormLabel>
                                            <FormControl>
                                                <Input type="password" placeholder="••••••••" {...field} className="bg-white/5 border-white/10 text-white h-11" />
                                            </FormControl>
                                            <FormMessage />
                                        </FormItem>
                                    )}
                                />



                                {error && (
                                    <motion.div
                                        initial={{ opacity: 0, y: -5 }} animate={{ opacity: 1, y: 0 }}
                                        className="p-3 bg-red-500/10 border border-red-500/20 text-red-200 text-sm rounded-md"
                                    >
                                        {error}
                                    </motion.div>
                                )}

                                <Button
                                    type="submit"
                                    className="w-full bg-gradient-to-r from-purple-600 to-blue-600 hover:from-purple-500 hover:to-blue-500 text-white border-0 h-11 font-medium shadow-lg shadow-purple-900/20 transition-all duration-300 hover:scale-[1.01]"
                                    disabled={isLoadingCredentials}
                                >
                                    {isLoadingCredentials ? (
                                        <>
                                            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                                            Connecting...
                                        </>
                                    ) : (
                                        <>
                                            Connect
                                            <ChevronRight className="ml-2 h-4 w-4 opacity-70" />
                                        </>
                                    )}
                                </Button>
                            </form>
                        </Form>
                    </CardContent>
                    <CardFooter className="flex justify-center py-6">

                    </CardFooter>
                </Card>
            </motion.div>
        </div>
    );
}
