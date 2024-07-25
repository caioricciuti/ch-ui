import axios from "axios";
import useAuthStore from "../stores/user.store";

const api = axios.create({
  baseURL: "http://localhost:5124/api/v1", // Adjust this to match your backend URL
  withCredentials: true,
  headers: {
    "Content-Type": "application/json",
  },
});

api.interceptors.response.use(
  (response) => response,
  async (error) => {
    const originalRequest = error.config;
    if (error.response.status === 401 && !originalRequest._retry) {
      originalRequest._retry = true;
      try {
        // Attempt to refresh the token
        await axios.post(
          api.defaults.baseURL + "/auth/refresh-token",
          {},
          { withCredentials: true }
        );
        // If successful, retry the original request
        return api(originalRequest);
      } catch (refreshError) {
        // If refresh fails, log out the user
        useAuthStore.getState().logout();
        throw refreshError;
      }
    }
    return Promise.reject(error);
  }
);

export default api;
