import clsx from "clsx";
import Heading from "@theme/Heading";
import styles from "./styles.module.css";

type FeatureItem = {
  title: string;
  // Svg: React.ComponentType<React.ComponentProps<"svg">>;
  description: JSX.Element;
};

const FeatureList: FeatureItem[] = [
  {
    title: "ClickHouse Integration",
    // Svg: require("@site/static/img/undraw_docusaurus_mountain.svg").default,
    description: (
      <>
        Interact seamlessly with ClickHouse databases. Manage connections,
        execute queries, and handle your data efficiently with our modern
        interface.
      </>
    ),
  },
  {
    title: "Dynamic UI Components",
    // Svg: require("@site/static/img/undraw_docusaurus_tree.svg").default,
    description: (
      <>
        Utilize advanced UI components for enhanced data interaction. Enjoy a
        responsive and intuitive experience while managing your ClickHouse
        databases.
      </>
    ),
  },
  {
    title: "Performance Optimizations",
    // Svg: require("@site/static/img/undraw_docusaurus_react.svg").default,
    description: (
      <>
        Efficient state management and optimized database interactions ensure
        smooth performance.
      </>
    ),
  },
  {
    title: "Backend Powered by Express & MongoDB",
    // Svg: require("@site/static/img/undraw_docusaurus_tree.svg").default,
    description: (
      <>
        The backend is built using Express.js and MongoDB, providing a robust
        and scalable foundation for managing your database operations.
      </>
    ),
  },
  {
    title: "Easy Installation with Docker",
    // Svg: require("@site/static/img/undraw_docusaurus_react.svg").default,
    description: (
      <>
        Our application is open-source and easy to install using Docker and
        Docker Compose. Get up and running quickly with our simple installation
        script.
      </>
    ),
  },
  {
    title: "Organization and User Management",
    // Svg: require("@site/static/img/undraw_docusaurus_tree.svg").default,
    description: (
      <>
        Manage organizations, users, and different connections effortlessly. Our
        app provides comprehensive management tools for all your needs.
      </>
    ),
  },
];

function Feature({ title, description }: FeatureItem) {
  return (
    <div className={clsx("col col--4")}>
      <div className="text--center">
        {/* <Svg className={styles.featureSvg} role="img" /> */}
      </div>
      <div className="text--center padding-horiz--md">
        <Heading as="h3">{title}</Heading>
        <p>{description}</p>
      </div>
    </div>
  );
}

export default function HomepageFeatures(): JSX.Element {
  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {FeatureList.map((props, idx) => (
            <Feature key={idx} {...props} />
          ))}
        </div>
      </div>
    </section>
  );
}
