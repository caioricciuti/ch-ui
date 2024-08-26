import React from "react";
import { FontAwesomeIcon } from "@fortawesome/react-fontawesome";
import { library } from "@fortawesome/fontawesome-svg-core";
import { fas } from "@fortawesome/free-solid-svg-icons";
import styles from "./HomepageFeatures/styles.module.css";

library.add(fas);

export const FeatureCard = ({ title, description, icon }) => (
  <div className={styles.feature_card}>
    <FontAwesomeIcon icon={icon} size="2x" />
    <h3>{title}</h3>
    <p>{description}</p>
  </div>
);

export const FeatureGrid = ({ children }) => (
  <div className={styles.feature_grid}>{children}</div>
);
