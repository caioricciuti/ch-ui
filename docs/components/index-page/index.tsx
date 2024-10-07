import { HeroSection } from "./HeroSection";
import HomepageFeatures from "./HomePageFeatures";

export const IndexPage = () => (
  <div className="container m-auto">
    <div className="home-content">
      <HeroSection />
      <HomepageFeatures />
    </div>
  </div>
);
