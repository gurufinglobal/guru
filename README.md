# GURUFIN Chain
 
GURUFIN Chain aims to seamlessly enable a multitude of services and business models for the real economy and the Web 3.0 environment by converging blockchain technology and traditional payment systems as the next-generation Layer-1 hybrid mainnet.

## 1. Definition
GURUFIN Mainnet constitutes a hybrid blockchain infrastructure that operates through the dualization of the <span style="color:orange">**Governance and Compliance Chains**</span>. This establishes a comprehensive and robust framework that facilitates efficient governance processing while operating compliance measures that are above current industry standards.
<p align="center"><img src="https://doc.gurufin.io/assets/01_GURUFIN_Chain.png" height="180px" width="800px"></p>

## 2. Layer-1 hybrid Chain
#### 2-1. Governance Chain
The Governance Chain uses the Tendermint Byzantine Fault Tolerance (BFT) and Delegated Proof-of-Stake (DPoS) consensus algorithm. GURU native token is issued to play a central role in creating a decentralized and democratic governance structure to meet user demands. Token holders delegate GURU to vote on proposals and decisions that affect the future development and direction of the network, share rewards with validators, and monitor validator integrity. External entities are encouraged to participate in node operation.
<p align="center"><img src="https://doc.gurufin.io/assets/02_Governance_Chain_Network_Configuration.png" height="240px" width="400px"></p>

#### 2-2. Compliance Chain
The Compliance Chain functions as a semi-private chain whereby GuruFin operates the Compliance and Sentry Nodes while the operation and delegation of Watcher Nodes are open to external entities. By employing the Tendermint BFT and Proof-of-Compliance (PoC) consensus algorithms, the network achieves streamlined transaction processing with remarkably low gas fees.

Upon receiving a transaction request, Compliance Nodes assume the responsibility of generating and validating blocks. Blocks are added to the Compliance Chain and monitored by Sentry Nodes for data integrity and potential corruption. Sentry Nodes share information with Watcher Nodes, upholding safeguards against potential threats.
Furthermore, the Compliance Chain provides a conducive environment for decentralized applications (dApps) and smart contracts by integrating the Ethereum Virtual Machine (EVM). This integration allows for seamless deployment and execution of smart contracts written in Turing complete programming languages, facilitating effortless migration to the Compliance Chain without requiring code modifications.
<p align="center"><img src="https://doc.gurufin.io/assets/03_Compliance_Chain_Network_Configuration.png" height="250px" width="400px"></p>

## 3. Capability
#### 3-1. Network Scalability:
- Interoperability between Zones (Station Feature).
- Token Swap Pool.
- ITMT Processing System.
#### 3-2. Fast Speed <span style="color:orange">**(10,000+ TPS)**</span>:
- Adjustable 1-3 Second Block Creation Time.
- TBFT & DPoS Consensus Algorithms.
- TBFT & PoC Consensus Algorithms.

## 4. Scalability
Projects can leverage the GuruFin SDK to create independent chains (or ‘Zones’). Zones have the autonomy to create unique ecosystems, protocols, and tokens within the broader GuruFin Ecosystem. By having control over the token infrastructure, projects can design and implement custom tokenomics models that align with their business models and ecosystem dynamics. APIs are provided for interoperability, allowing communication between Zones through the Station. GuruFin plans to expand the ecosystem by opening IBC channels to communicate between GuruFin Station and Cosmos Hub.
<p align="center"><img src="https://doc.gurufin.io/assets/04_Station_Architecture.png" height="300px" width="570px"></p>

## 5. Core Technology
#### 5-1. ITMT (Inter-Transaction Multi-Transfer) Processing System:  
GURUFIN's patent-pending Inter-Transaction Multi-Transfer (ITMT) Processing System enables simultaneous transfers to multiple payees. The ITMT Processing System records multiple transaction lists in a smart contract of an entire node system in advance and generates transactions for remittances simultaneously when the predefined conditions are met. This reduces the processing time and costs for large volumes and amounts of remittance transactions normally seen in traditional finance and airdrops.

GURUFIN has also designed a comprehensive remittance system that works in tandem with blockchain technology to meet the standard of approval in the traditional banking industry, expanding the benefits of the ITMT Processing System into the traditional finance sector. The combination of 17 PoC and ITMT technology increases transparency and reduces vulnerabilities to fraud in banking systems through the adoption of blockchain technology devised for partial or complete transition to decentralization.
<p align="center"><img src="https://doc.gurufin.io/assets/06_ITMT_Process.png" height="310px" width="570px"></p>

#### 5-2. DARK (Divided Authority and Recovery Key) Security System:  
DARK Security System is patent-pending technology that applies a mnemonic-based multisignature authentication method to the GURU Wallet. Users are prompted to choose one of the randomly generated 24 words to create and encrypt their mnemonic. 

The chosen word (or ‘mnemonic word’) generates the first signature key and is stored by the user. The remaining 23 words are generated as the second signature key and stored in the system. This is referred to as the dual-seed (or ‘key-splitting’) process. 

Traditional mnemonics phrases encrypt a 12-to-24-word sequence with a single centralized encryption key, which heightens the chance of being hacked and its assets stolen. Furthermore, users have the disadvantage of having to remember the number and array of words in the sequence. Forgotten mnemonics result in irrevocable loss of assets.
<p align="center"><img src="https://doc.gurufin.io/assets/07_DARK_1.png" height=100px" width="570px"></p>
<p align="center"><img src="https://doc.gurufin.io/assets/08_Dark_2.png" height=500px" width="570px"></p>

#### 5-3. Personalized Recommendation System:
Up to Depth 10 of metadata can be extracted from NFTs, user profile, and user behavior for in-depth analysis and insight on NFT transactions and user engagement. By employing advanced analytics techniques, patterns and trends can be identified to personalize and optimize marketing strategies. This analysis is conducted with the goal of enhancing the overall user experience and ultimately driving revenue of NFT-related projects. 

Through data mining, marketers can gain valuable insight into user preferences, interests, and behaviors. This information enables the creation of tailored marketing campaigns that resonate with the target audience, increasing the likelihood of user engagement and conversion. By personalizing marketing efforts, users are more likely to discover NFTs and projects that align with their interests, fostering a stronger connection between users and the NFT ecosystem. 

GURUFIN adhere to strict privacy guidelines and regulations such as the General Data Protection Regulation (GDPR) in the European Union regarding data management. Data is anonymized to ensure the protection of individual identities to maintain user privacy. Compliance with GDPR safeguards against potential misuse or unauthorized access to personal information.
<p align="center"><img src="https://doc.gurufin.io/assets/09_PERSONALIZATION.png" height=480px" width="800px"></p>

#### 5-4. Payment System:
The integration of a traditional Payment Service Provider (PSP) into the GURUFIN System brings seamless purchasing capabilities to consumers within the Web 3.0 environment. This integration allows users to conveniently acquire GURU, MU, ERC-20, and ERC-721 tokens using various payment methods for fiat currency transactions. 

The availability of fiat currency transactions enables users to purchase tokens directly, eliminating the need for additional steps such as downloading decentralized finance (DeFi) applications or converting fiat to cryptocurrency before engaging with the Web 3.0 ecosystem. By accepting payment instruments such as cards and carrier billing, the system simplifies the purchasing process and reduces the entry barrier for individuals who may be interested in Web 3.0 but feel intimidated by its complexities. By incorporating a real economy PSP, the GURUFIN System ensures a secure and reliable payment process. 

Users can confidently transact knowing that their payment information is handled by a trusted and regulated PSP, adhering to industry standards and security protocols. Furthermore, the integration with the blockchain ensures that every purchase, along with its details, is recorded for a verifiable and auditable history of transactions.
<p align="center"><img src="https://doc.gurufin.io/assets/05_PSP_Integration_Payment_System_Structure.png" height="300px" width="620px"></p>

## 6. GURU Explorer
[Go to the Guru Explorer](https://scan.gurufin.io/)
