// Sponsors component

const SponsorsList = [
  {
    name: "Ibero Data",
    logo: "https://www.iberodata.es/logo.png",
    url: "https://www.iberodata.es/?utm_source=ch-ui&utm_medium=sponsorship",
  },
];

export const Sponsors = () => {
  return (
    <section id="sponsors" className="py-12 w-full">
      <div className="w-full">
        <div className="text-center mb-12 md:mb-18">
          <h2 className="text-4xl  font-extrabold tracking-tight sm:text-5xl md:text-6xl lg:text-7xl bg-clip-text text-transparent bg-gradient-to-r from-orange-700 via-pink-600 to-orange-300">
            Sponsors
          </h2>
          <p className="text-lg text-gray-600 mt-4">
            We would like to thank our sponsors for their support.
          </p>
        </div>
        <div className="flex flex-wrap justify-center">
          {SponsorsList.map((sponsor, index) => (
            <a
              key={index}
              href={sponsor.url}
              target="_blank"
              rel="noopener noreferrer"
              className="flex flex-col items-center justify-center"
            >
              <img
                src={sponsor.logo}
                alt={sponsor.name}
                className="w-32 h-32 object-contain mx-4 my-4"
              />
              <span className="font-bold text-xl">{sponsor.name}</span>
            </a>
          ))}
        </div>
      </div>
    </section>
  );
};

export default Sponsors;
