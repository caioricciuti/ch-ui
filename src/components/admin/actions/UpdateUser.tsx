import React from 'react';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
    DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import useAppStore from "@/store/appStore";
import { toast } from 'sonner'

interface UserData {
    name: string;
    id: string;
    auth_type: string[];
    host_ip: string[];
    default_roles_list: string[];
    default_database: string;
}

interface UpdateUserProps {
    user: UserData;
    onClose: () => void;
    onUpdated: () => void;
}

const UpdateUser: React.FC<UpdateUserProps> = ({ user, onClose, onUpdated }) => {
    const { runQuery } = useAppStore();
    const [authType, setAuthType] = React.useState(user.auth_type.join(", "));
    const [hostIp, setHostIp] = React.useState(user.host_ip.join(", "));
    const [defaultRolesList, setDefaultRolesList] = React.useState(user.default_roles_list.join(", "));
    const [defaultDatabase, setDefaultDatabase] = React.useState(user.default_database);
    const [loading, setLoading] = React.useState(false);

    const handleUpdateUser = async () => {
        setLoading(true);
        try {
            const authTypeArray = authType.split(",").map((item) => item.trim());
            const hostIpArray = hostIp.split(",").map((item) => item.trim());
            const rolesArray = defaultRolesList.split(",").map((item) => item.trim());

            await runQuery(`
                ALTER USER ${user.name} 
                SET AUTH TYPE ${authTypeArray.map((type) => `'${type}'`).join(", ")},
                HOST IP ${hostIpArray.join(", ")},
                DEFAULT ROLE ${rolesArray.join(", ")},
                DEFAULT DATABASE '${defaultDatabase}'
            `);

            toast.info(`User ${user.name} updated successfully.`);
            onUpdated();
            onClose();
        } catch (error: any) {
            toast.error(`Failed to update user: ${error.message}`);
        } finally {
            setLoading(false);
        }
    };

    return (
        <Dialog open={true} onOpenChange={onClose}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>Update User - {user.name}</DialogTitle>
                    <DialogDescription>
                        Update the details for the user "{user.name}" below.
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-4">
                    <div>
                        <label className="block text-sm font-medium text-gray-700">Authentication Type</label>
                        <Input
                            value={authType}
                            onChange={(e) => setAuthType(e.target.value)}
                            placeholder="e.g., Password, LDAP"
                        />
                    </div>
                    <div>
                        <label className="block text-sm font-medium text-gray-700">Host IP</label>
                        <Input
                            value={hostIp}
                            onChange={(e) => setHostIp(e.target.value)}
                            placeholder="e.g., 192.168.1.1, 192.168.1.2"
                        />
                    </div>
                    <div>
                        <label className="block text-sm font-medium text-gray-700">Default Roles</label>
                        <Input
                            value={defaultRolesList}
                            onChange={(e) => setDefaultRolesList(e.target.value)}
                            placeholder="e.g., admin, editor"
                        />
                    </div>
                    <div>
                        <label className="block text-sm font-medium text-gray-700">Default Database</label>
                        <Input
                            value={defaultDatabase}
                            onChange={(e) => setDefaultDatabase(e.target.value)}
                            placeholder="e.g., my_database"
                        />
                    </div>
                </div>

                <DialogFooter>
                    <Button variant="outline" onClick={onClose} disabled={loading}>
                        Cancel
                    </Button>
                    <Button
                        variant="outline"
                        onClick={handleUpdateUser}
                        disabled={loading}
                    >
                        {loading ? "Updating..." : "Update User"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
};

export default UpdateUser;
