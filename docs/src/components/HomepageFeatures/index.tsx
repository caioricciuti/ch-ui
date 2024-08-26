import React, { useState, useEffect } from "react";
import { motion } from "framer-motion";
import {
  FaDatabase,
  FaDesktop,
  FaRocket,
  FaServer,
  FaDocker,
  FaUsers,
} from "react-icons/fa";
import styles from "./styles.module.css";

type FeatureItem = {
  title: string;
  icon: React.ElementType;
  description: string;
};

const FeatureList: FeatureItem[] = [
  {
    title: "ClickHouse Integration",
    icon: FaDatabase,
    description:
      "Interact seamlessly with ClickHouse databases. Manage connections, execute queries, and handle your data efficiently with our modern interface.",
  },
  {
    title: "Dynamic UI Components",
    icon: FaDesktop,
    description:
      "Utilize advanced UI components for enhanced data interaction. Enjoy a responsive and intuitive experience while managing your ClickHouse databases.",
  },
  {
    title: "Performance Optimizations",
    icon: FaRocket,
    description:
      "Efficient state management and optimized database interactions ensure smooth performance.",
  },
  {
    title: "Backend Powered by Express & MongoDB",
    icon: FaServer,
    description:
      "The backend is built using Express.js and MongoDB, providing a robust and scalable foundation for managing your database operations.",
  },
  {
    title: "Easy Installation",
    icon: FaDocker,
    description:
      "Our application is open-source and easy to install. Get up and running quickly with our simple installation script.",
  },
  {
    title: "Organization and User Management",
    icon: FaUsers,
    description:
      "Manage organizations, users, and different connections effortlessly. Our app provides comprehensive management tools for all your needs.",
  },
];

const Feature: React.FC<FeatureItem> = ({ title, icon: Icon, description }) => {
  const [isHovered, setIsHovered] = useState(false);

  return (
    <motion.div
      className={`${styles.feature} col col--4`}
      initial={{ opacity: 0, y: 50 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5 }}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
    >
      <div className={styles.featureIcon}>
        <Icon size={48} color={isHovered ? "#E8B809" : "#444"} />
      </div>
      <h3 className={styles.featureTitle}>{title}</h3>
      <p className={styles.featureDescription}>{description}</p>
    </motion.div>
  );
};

const HomepageFeatures: React.FC = () => {
  const [visibleFeatures, setVisibleFeatures] = useState(3);

  useEffect(() => {
    const handleScroll = () => {
      const scrollPosition = window.innerHeight + window.scrollY;
      const pageHeight = document.documentElement.offsetHeight;
      if (
        scrollPosition >= pageHeight - 500 &&
        visibleFeatures < FeatureList.length
      ) {
        setVisibleFeatures((prev) => Math.min(prev + 3, FeatureList.length));
      }
    };

    window.addEventListener("scroll", handleScroll);
    return () => window.removeEventListener("scroll", handleScroll);
  }, [visibleFeatures]);

  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {FeatureList.slice(0, visibleFeatures).map((props, idx) => (
            <Feature key={idx} {...props} />
          ))}
        </div>
        {visibleFeatures < FeatureList.length && (
          <div className={styles.loadMore}>
            <button
              onClick={() =>
                setVisibleFeatures((prev) =>
                  Math.min(prev + 3, FeatureList.length)
                )
              }
            >
              Load More Features
            </button>
          </div>
        )}
      </div>
    </section>
  );
};

export default HomepageFeatures;
